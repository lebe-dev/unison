//! Download of a release and the verification of its signature and hash.
//!
//! The manifest is trusted only after its Ed25519 signature matches the key
//! embedded at compile time. Only then is an artifact streamed, hashed and
//! moved into the staging directory, and the part file is removed on every
//! failure or cancellation.

use std::fs;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::{Duration, Instant, SystemTime};

use base64::Engine;
use base64::engine::general_purpose::STANDARD as BASE64;
use ed25519_dalek::{Signature, VerifyingKey};
use futures_util::StreamExt;
use sha2::{Digest, Sha256};
use tauri::Manager;

use super::github::ReleaseInfo;

/// The signature covers the manifest behind this prefix, so a signature of one
/// document cannot be replayed as another.
const SIGNATURE_HEAD: &[u8] = b"unison/1 release\x00";

/// Largest accepted manifest.
const MANIFEST_MAX_BYTES: u64 = 256 * 1024;

/// Largest accepted signature file.
const SIGNATURE_MAX_BYTES: u64 = 4 * 1024;

/// Largest accepted artifact. The installers of Unison stay far below it.
pub const ARTIFACT_MAX_BYTES: u64 = 256 * 1024 * 1024;

/// A staging directory older than this is removed at startup.
const STALE_AGE: Duration = Duration::from_secs(24 * 60 * 60);

/// How often a download reports its progress.
const PROGRESS_INTERVAL: Duration = Duration::from_millis(150);

/// Limits of a download, reduced in the tests.
#[derive(Clone, Copy)]
struct Limits {
    artifact: u64,
    manifest: u64,
    signature: u64,
}

impl Default for Limits {
    fn default() -> Self {
        Self {
            artifact: ARTIFACT_MAX_BYTES,
            manifest: MANIFEST_MAX_BYTES,
            signature: SIGNATURE_MAX_BYTES,
        }
    }
}

/// VerifyError is a failed download or verification, with a code the window
/// translates.
#[derive(Clone, Debug)]
pub struct VerifyError {
    pub code: String,
    pub message: String,
}

impl VerifyError {
    /// new returns an error with a code and a message.
    fn new(code: &str, message: impl Into<String>) -> Self {
        Self {
            code: code.to_string(),
            message: message.into(),
        }
    }
}

/// staging_root returns the directory of the downloads of the updater.
pub fn staging_root(app: &tauri::AppHandle) -> Result<PathBuf, VerifyError> {
    app.path()
        .app_cache_dir()
        .map(|dir| dir.join("updates"))
        .map_err(|err| VerifyError::new("cache_dir", format!("cannot find the cache: {err}")))
}

/// purge_stale removes the staging directories that a previous run left behind.
pub fn purge_stale(root: &Path) {
    let Ok(entries) = fs::read_dir(root) else {
        return;
    };
    let now = SystemTime::now();
    for entry in entries.flatten() {
        let path = entry.path();
        if !entry.file_type().map(|kind| kind.is_dir()).unwrap_or(false) {
            continue;
        }
        let modified = entry
            .metadata()
            .and_then(|meta| meta.modified())
            .unwrap_or(SystemTime::UNIX_EPOCH);
        let age = now.duration_since(modified).unwrap_or_default();
        if age >= STALE_AGE {
            let _ = fs::remove_dir_all(&path);
        }
    }
}

/// public_key returns the key that verifies a release. It fails when the build
/// embedded no key, so a broken build never accepts an unsigned release.
pub fn public_key() -> Result<VerifyingKey, VerifyError> {
    from_base64(env!("UNISON_UPDATE_PUBLIC_KEY"))
}

/// from_base64 decodes a public key.
fn from_base64(text: &str) -> Result<VerifyingKey, VerifyError> {
    let text = text.trim();
    if text.is_empty() {
        return Err(VerifyError::new(
            "no_public_key",
            "this build carries no key to verify an update",
        ));
    }
    let raw = BASE64.decode(text).map_err(|err| {
        VerifyError::new(
            "public_key_invalid",
            format!("the key is not base64: {err}"),
        )
    })?;
    let bytes: [u8; 32] = raw
        .try_into()
        .map_err(|_| VerifyError::new("public_key_invalid", "the key is not 32 bytes"))?;
    VerifyingKey::from_bytes(&bytes)
        .map_err(|err| VerifyError::new("public_key_invalid", format!("the key is invalid: {err}")))
}

/// download_and_verify downloads the artifact of a release into the staging
/// directory and returns the path of the verified file.
pub async fn download_and_verify<P>(
    http: &reqwest::Client,
    root: &Path,
    release: &ReleaseInfo,
    key: &VerifyingKey,
    cancel: &AtomicBool,
    progress: P,
) -> Result<PathBuf, VerifyError>
where
    P: Fn(u64, u64),
{
    download_with(
        http,
        root,
        release,
        key,
        cancel,
        Limits::default(),
        progress,
    )
    .await
}

/// download_with is download_and_verify with the limits of a test.
#[allow(clippy::too_many_arguments)]
async fn download_with<P>(
    http: &reqwest::Client,
    root: &Path,
    release: &ReleaseInfo,
    key: &VerifyingKey,
    cancel: &AtomicBool,
    limits: Limits,
    progress: P,
) -> Result<PathBuf, VerifyError>
where
    P: Fn(u64, u64),
{
    let name = safe_name(&release.asset_name)?;
    let staging = root.join(&release.summary.version);
    create_private(&staging)?;

    let sums = fetch(http, &release.checksums_url, limits.manifest, cancel).await?;
    let signature = fetch(http, &release.signature_url, limits.signature, cancel).await?;
    verify_signature(key, &sums, &signature)?;
    let expected = expected_digest(&sums, &name)?;

    let path = staging.join(&name);
    let part = staging.join(format!("{name}.part"));
    let result = stream_artifact(
        http,
        &release.asset_url,
        &part,
        &expected,
        limits.artifact,
        cancel,
        progress,
        release.summary.asset_size,
    )
    .await;
    match result {
        Ok(()) => {
            fs::rename(&part, &path).map_err(|err| {
                let _ = fs::remove_file(&part);
                VerifyError::new("staging", format!("cannot store the update: {err}"))
            })?;
            Ok(path)
        }
        Err(err) => {
            let _ = fs::remove_file(&part);
            Err(err)
        }
    }
}

/// stream_artifact downloads the artifact into the part file while it hashes
/// it.
#[allow(clippy::too_many_arguments)]
async fn stream_artifact<P>(
    http: &reqwest::Client,
    url: &str,
    part: &Path,
    expected: &[u8; 32],
    max_bytes: u64,
    cancel: &AtomicBool,
    progress: P,
    announced_total: u64,
) -> Result<(), VerifyError>
where
    P: Fn(u64, u64),
{
    if cancel.load(Ordering::Relaxed) {
        return Err(cancelled());
    }
    let response =
        http.get(url).send().await.map_err(|err| {
            VerifyError::new("network", format!("cannot download the update: {err}"))
        })?;
    if !response.status().is_success() {
        return Err(VerifyError::new(
            "download_failed",
            format!(
                "the download answered with the status {}",
                response.status().as_u16()
            ),
        ));
    }
    let total = response.content_length().unwrap_or(announced_total);
    if total > max_bytes {
        return Err(too_large(max_bytes));
    }

    let mut file = fs::File::create(part)
        .map_err(|err| VerifyError::new("staging", format!("cannot store the update: {err}")))?;
    let mut hasher = Sha256::new();
    let mut downloaded: u64 = 0;
    let mut reported = Instant::now() - PROGRESS_INTERVAL;
    let mut stream = response.bytes_stream();
    while let Some(chunk) = stream.next().await {
        if cancel.load(Ordering::Relaxed) {
            return Err(cancelled());
        }
        let chunk = chunk
            .map_err(|err| VerifyError::new("network", format!("the download stopped: {err}")))?;
        downloaded += chunk.len() as u64;
        if downloaded > max_bytes {
            return Err(too_large(max_bytes));
        }
        file.write_all(&chunk).map_err(|err| {
            VerifyError::new("staging", format!("cannot store the update: {err}"))
        })?;
        hasher.update(&chunk);
        if reported.elapsed() >= PROGRESS_INTERVAL {
            reported = Instant::now();
            progress(downloaded, total);
        }
    }
    file.flush()
        .and_then(|()| file.sync_all())
        .map_err(|err| VerifyError::new("staging", format!("cannot store the update: {err}")))?;
    drop(file);

    let digest = hasher.finalize();
    if digest.as_slice() != expected {
        return Err(VerifyError::new(
            "checksum_mismatch",
            "the downloaded update does not match the signed manifest",
        ));
    }
    progress(downloaded, total);
    Ok(())
}

/// fetch reads a small file into memory without exceeding its limit.
async fn fetch(
    http: &reqwest::Client,
    url: &str,
    max_bytes: u64,
    cancel: &AtomicBool,
) -> Result<Vec<u8>, VerifyError> {
    let response = http
        .get(url)
        .send()
        .await
        .map_err(|err| VerifyError::new("network", format!("cannot download a file: {err}")))?;
    if !response.status().is_success() {
        return Err(VerifyError::new(
            "download_failed",
            format!(
                "a download answered with the status {}",
                response.status().as_u16()
            ),
        ));
    }
    if response
        .content_length()
        .is_some_and(|length| length > max_bytes)
    {
        return Err(too_large(max_bytes));
    }
    let mut buffer = Vec::new();
    let mut stream = response.bytes_stream();
    while let Some(chunk) = stream.next().await {
        if cancel.load(Ordering::Relaxed) {
            return Err(cancelled());
        }
        let chunk = chunk
            .map_err(|err| VerifyError::new("network", format!("a download stopped: {err}")))?;
        if buffer.len() as u64 + chunk.len() as u64 > max_bytes {
            return Err(too_large(max_bytes));
        }
        buffer.extend_from_slice(&chunk);
    }
    Ok(buffer)
}

/// verify_signature checks the manifest against the public key.
fn verify_signature(
    key: &VerifyingKey,
    manifest: &[u8],
    signature: &[u8],
) -> Result<(), VerifyError> {
    let signature = BASE64
        .decode(String::from_utf8_lossy(signature).trim())
        .map_err(|err| {
            VerifyError::new(
                "signature_invalid",
                format!("the signature is not base64: {err}"),
            )
        })?;
    let signature = Signature::from_slice(&signature).map_err(|err| {
        VerifyError::new(
            "signature_invalid",
            format!("the signature is invalid: {err}"),
        )
    })?;
    let mut message = Vec::with_capacity(SIGNATURE_HEAD.len() + manifest.len());
    message.extend_from_slice(SIGNATURE_HEAD);
    message.extend_from_slice(manifest);
    key.verify_strict(&message, &signature).map_err(|_| {
        VerifyError::new(
            "signature_invalid",
            "the signature of the manifest is wrong",
        )
    })
}

/// expected_digest returns the hash of the artifact from the manifest.
fn expected_digest(manifest: &[u8], name: &str) -> Result<[u8; 32], VerifyError> {
    let text = String::from_utf8_lossy(manifest);
    for line in text.lines() {
        let Some((digest, entry)) = line.split_once("  ") else {
            continue;
        };
        if entry != name {
            continue;
        }
        return hex32(digest).ok_or_else(|| {
            VerifyError::new("checksums_invalid", "the manifest holds a malformed hash")
        });
    }
    Err(VerifyError::new(
        "checksums_missing",
        "the manifest names no artifact of this platform",
    ))
}

/// hex32 parses a lowercase or uppercase SHA-256 hash.
fn hex32(text: &str) -> Option<[u8; 32]> {
    let bytes = text.as_bytes();
    if bytes.len() != 64 {
        return None;
    }
    let mut out = [0u8; 32];
    for (index, pair) in bytes.as_chunks::<2>().0.iter().enumerate() {
        let high = hex_value(pair[0])?;
        let low = hex_value(pair[1])?;
        out[index] = (high << 4) | low;
    }
    Some(out)
}

/// hex_value returns the value of one hex digit.
fn hex_value(byte: u8) -> Option<u8> {
    match byte {
        b'0'..=b'9' => Some(byte - b'0'),
        b'a'..=b'f' => Some(byte - b'a' + 10),
        b'A'..=b'F' => Some(byte - b'A' + 10),
        _ => None,
    }
}

/// safe_name returns the file name of an artifact and refuses a name that
/// leaves the staging directory.
fn safe_name(name: &str) -> Result<String, VerifyError> {
    if name.is_empty() || name == "." || name == ".." || name.contains(['/', '\\']) {
        return Err(VerifyError::new(
            "artifact_invalid",
            "the artifact has a bad name",
        ));
    }
    Ok(name.to_string())
}

/// create_private creates the staging directory, reachable only by the user.
fn create_private(path: &Path) -> Result<(), VerifyError> {
    fs::create_dir_all(path).map_err(|err| {
        VerifyError::new(
            "staging",
            format!("cannot create the staging directory: {err}"),
        )
    })?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(path, fs::Permissions::from_mode(0o700)).map_err(|err| {
            VerifyError::new(
                "staging",
                format!("cannot restrict the staging directory: {err}"),
            )
        })?;
    }
    Ok(())
}

/// cancelled returns the error of a download the user stopped.
fn cancelled() -> VerifyError {
    VerifyError::new("cancelled", "the download was cancelled")
}

/// too_large returns the error of a file above its limit.
fn too_large(limit: u64) -> VerifyError {
    VerifyError::new(
        "too_large",
        format!("a file is larger than the limit of {limit} bytes"),
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::testutil::TempDir;
    use crate::updater::github::ReleaseSummary;
    use crate::updater::testserver::{MockRequest, MockServer, Reply};
    use ed25519_dalek::{Signer, SigningKey};
    use std::collections::HashMap;
    use std::sync::Arc;
    use std::sync::atomic::AtomicUsize;

    const ARTIFACT: &[u8] = b"the disk image of Unison 0.3.0";
    const NAME: &str = "unison-v0.3.0-macos.dmg";

    /// signing_key returns a deterministic key of a test.
    fn signing_key(seed: u8) -> SigningKey {
        SigningKey::from_bytes(&[seed; 32])
    }

    /// sha returns the hash of a body.
    fn sha(body: &[u8]) -> String {
        let mut hasher = Sha256::new();
        hasher.update(body);
        let digest = hasher.finalize();
        digest.iter().map(|byte| format!("{byte:02x}")).collect()
    }

    /// signed_headers returns the manifest and the signature of a test.
    fn signed(seed: u8, body: &[u8], name: &str) -> (Vec<u8>, Vec<u8>) {
        let key = signing_key(seed);
        let manifest = format!("{}  {name}\n", sha(body));
        let mut message = SIGNATURE_HEAD.to_vec();
        message.extend_from_slice(manifest.as_bytes());
        let signature = key.sign(&message);
        let text = format!("{}\n", BASE64.encode(signature.to_bytes()));
        (manifest.into_bytes(), text.into_bytes())
    }

    /// release names the artifact of a test.
    fn release(base: &str) -> ReleaseInfo {
        ReleaseInfo {
            summary: ReleaseSummary {
                version: "0.3.0".to_string(),
                name: "Unison 0.3.0".to_string(),
                body: String::new(),
                prerelease: false,
                published_at: String::new(),
                html_url: String::new(),
                asset_size: ARTIFACT.len() as u64,
            },
            tag_name: "0.3.0".to_string(),
            asset_name: NAME.to_string(),
            asset_url: format!("{base}/{NAME}"),
            checksums_url: format!("{base}/SHA256SUMS"),
            signature_url: format!("{base}/SHA256SUMS.sig"),
        }
    }

    /// server serves the files of a test by path.
    async fn server(files: HashMap<String, Vec<u8>>, hits: Arc<AtomicUsize>) -> MockServer {
        MockServer::start(move |request: &MockRequest| {
            if request.path.ends_with(NAME) {
                hits.fetch_add(1, Ordering::Relaxed);
            }
            match files.get(&request.path) {
                Some(body) => Reply::ok(body.clone()),
                None => Reply {
                    status: 404,
                    headers: Vec::new(),
                    body: Vec::new(),
                },
            }
        })
        .await
    }

    /// files returns the files of a valid release.
    fn files(body: &[u8]) -> HashMap<String, Vec<u8>> {
        let (manifest, signature) = signed(7, body, NAME);
        HashMap::from([
            ("/SHA256SUMS".to_string(), manifest),
            ("/SHA256SUMS.sig".to_string(), signature),
            (format!("/{NAME}"), body.to_vec()),
        ])
    }

    #[tokio::test]
    async fn a_verified_release_is_stored() {
        let hits = Arc::new(AtomicUsize::new(0));
        let server = server(files(ARTIFACT), Arc::clone(&hits)).await;
        let dir = TempDir::new();
        let key = signing_key(7).verifying_key();
        let cancel = AtomicBool::new(false);

        let path = download_and_verify(
            &super::super::github::client().expect("client"),
            dir.path(),
            &release(server.base()),
            &key,
            &cancel,
            |_, _| {},
        )
        .await
        .expect("download");

        assert_eq!(fs::read(&path).expect("read"), ARTIFACT);
        assert_eq!(hits.load(Ordering::Relaxed), 1);
    }

    #[tokio::test]
    async fn a_bad_signature_stops_before_the_artifact() {
        let hits = Arc::new(AtomicUsize::new(0));
        let mut entries = files(ARTIFACT);
        let (_, signature) = signed(8, ARTIFACT, NAME);
        entries.insert("/SHA256SUMS.sig".to_string(), signature);
        let server = server(entries, Arc::clone(&hits)).await;
        let dir = TempDir::new();
        let key = signing_key(7).verifying_key();
        let cancel = AtomicBool::new(false);

        let err = download_and_verify(
            &super::super::github::client().expect("client"),
            dir.path(),
            &release(server.base()),
            &key,
            &cancel,
            |_, _| {},
        )
        .await
        .expect_err("a bad signature");

        assert_eq!(err.code, "signature_invalid");
        assert_eq!(hits.load(Ordering::Relaxed), 0);
    }

    #[tokio::test]
    async fn a_wrong_hash_removes_the_part_file() {
        let hits = Arc::new(AtomicUsize::new(0));
        let (manifest, signature) = signed(7, b"another artifact", NAME);
        let entries = HashMap::from([
            ("/SHA256SUMS".to_string(), manifest),
            ("/SHA256SUMS.sig".to_string(), signature),
            (format!("/{NAME}"), ARTIFACT.to_vec()),
        ]);
        let server = server(entries, Arc::clone(&hits)).await;
        let dir = TempDir::new();
        let key = signing_key(7).verifying_key();
        let cancel = AtomicBool::new(false);

        let err = download_and_verify(
            &super::super::github::client().expect("client"),
            dir.path(),
            &release(server.base()),
            &key,
            &cancel,
            |_, _| {},
        )
        .await
        .expect_err("a mismatch");

        assert_eq!(err.code, "checksum_mismatch");
        let staging = dir.path().join("0.3.0");
        assert!(!staging.join(format!("{NAME}.part")).exists());
        assert!(!staging.join(NAME).exists());
    }

    #[tokio::test]
    async fn a_cancelled_download_stores_nothing() {
        let hits = Arc::new(AtomicUsize::new(0));
        let server = server(files(ARTIFACT), Arc::clone(&hits)).await;
        let dir = TempDir::new();
        let key = signing_key(7).verifying_key();
        let cancel = AtomicBool::new(true);

        let err = download_and_verify(
            &super::super::github::client().expect("client"),
            dir.path(),
            &release(server.base()),
            &key,
            &cancel,
            |_, _| {},
        )
        .await
        .expect_err("a cancelled download");

        assert_eq!(err.code, "cancelled");
        let staging = dir.path().join("0.3.0");
        assert!(!staging.join(format!("{NAME}.part")).exists());
    }

    #[tokio::test]
    async fn an_oversize_artifact_aborts() {
        let hits = Arc::new(AtomicUsize::new(0));
        let server = server(files(ARTIFACT), Arc::clone(&hits)).await;
        let dir = TempDir::new();
        let key = signing_key(7).verifying_key();
        let cancel = AtomicBool::new(false);
        let lim = Limits {
            artifact: 4,
            ..Limits::default()
        };

        let err = download_with(
            &super::super::github::client().expect("client"),
            dir.path(),
            &release(server.base()),
            &key,
            &cancel,
            lim,
            |_, _| {},
        )
        .await
        .expect_err("an oversize artifact");

        assert_eq!(err.code, "too_large");
        let staging = dir.path().join("0.3.0");
        assert!(!staging.join(format!("{NAME}.part")).exists());
    }

    #[test]
    fn the_manifest_names_the_artifact() {
        let digest = sha(ARTIFACT);
        let manifest = format!("{digest}  {NAME}\n");
        assert!(expected_digest(manifest.as_bytes(), NAME).is_ok());
        assert!(expected_digest(manifest.as_bytes(), "other.dmg").is_err());
        assert!(expected_digest(b"not a manifest\n", NAME).is_err());
    }

    #[test]
    fn an_artifact_name_stays_in_the_directory() {
        assert!(safe_name(NAME).is_ok());
        assert!(safe_name("../escape").is_err());
        assert!(safe_name("nested/file").is_err());
        assert!(safe_name("").is_err());
    }
}
