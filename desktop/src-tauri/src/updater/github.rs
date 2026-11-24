//! The releases of GitHub and the selection of the one to offer.
//!
//! Every URL of an artifact is checked against the hosts of GitHub before it is
//! used, and the client refuses a redirect to any other host.

use std::time::Duration;

use serde::{Deserialize, Serialize};

use super::config::UpdaterConfig;
use super::version::Version;

/// Address of the releases of Unison.
pub const DEFAULT_API_URL: &str =
    "https://api.github.com/repos/lebe-dev/unison/releases?per_page=20";

/// How long the metadata request may take.
const METADATA_TIMEOUT: Duration = Duration::from_secs(15);

/// How long a download may take in total.
const DOWNLOAD_TIMEOUT: Duration = Duration::from_secs(10 * 60);

/// The changelog of a release is cut here, so a huge body stays manageable.
const MAX_BODY_BYTES: usize = 128 * 1024;

/// Hosts whose URLs may carry an artifact.
const ALLOWED_HOSTS: [&str; 4] = [
    "github.com",
    "api.github.com",
    "objects.githubusercontent.com",
    "release-assets.githubusercontent.com",
];

/// ReleaseSummary is what the window shows about a release.
#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ReleaseSummary {
    /// Version without the leading `v`.
    pub version: String,
    /// Name of the release.
    pub name: String,
    /// Changelog in Markdown.
    pub body: String,
    /// Whether the release is a GitHub pre-release.
    pub prerelease: bool,
    /// Time GitHub published the release.
    pub published_at: String,
    /// Page of the release on GitHub.
    pub html_url: String,
    /// Size of the artifact in bytes.
    pub asset_size: u64,
}

/// ReleaseInfo is a release with the addresses the shell downloads from.
#[derive(Clone, Debug)]
pub struct ReleaseInfo {
    pub summary: ReleaseSummary,
    pub tag_name: String,
    pub asset_name: String,
    pub asset_url: String,
    pub checksums_url: String,
    pub signature_url: String,
}

/// CheckOutcome is the answer of one check.
#[derive(Clone, Debug)]
pub struct CheckOutcome {
    /// Whether GitHub answered that nothing changed.
    pub not_modified: bool,
    /// The release to offer, when one is newer.
    pub release: Option<ReleaseInfo>,
    /// Entity tag of the answer, kept for the next check.
    pub etag: Option<String>,
}

/// CheckError is a check that failed, with a code the window translates.
#[derive(Clone, Debug)]
pub struct CheckError {
    pub code: String,
    pub message: String,
    /// Time of the next possible check after a rate limit, in unix
    /// milliseconds.
    pub retry_after_unix_ms: Option<i64>,
}

impl CheckError {
    /// new returns an error with a code and a message.
    pub fn new(code: &str, message: impl Into<String>) -> Self {
        Self {
            code: code.to_string(),
            message: message.into(),
            retry_after_unix_ms: None,
        }
    }

    /// at adds the time of the next possible check.
    pub fn at(mut self, retry_after_unix_ms: Option<i64>) -> Self {
        self.retry_after_unix_ms = retry_after_unix_ms;
        self
    }
}

/// client builds the HTTP client of the updater. A redirect may only lead to a
/// host of GitHub, which keeps a compromised answer from moving a download to
/// another server.
pub fn client() -> Result<reqwest::Client, String> {
    reqwest::Client::builder()
        .connect_timeout(METADATA_TIMEOUT)
        .timeout(DOWNLOAD_TIMEOUT)
        .redirect(reqwest::redirect::Policy::custom(|attempt| {
            if attempt.previous().len() >= 10 {
                attempt.stop()
            } else if allowed_url(attempt.url().as_str()) {
                attempt.follow()
            } else {
                attempt.error("the redirect leaves the hosts of GitHub")
            }
        }))
        .build()
        .map_err(|err| format!("cannot build the HTTP client of the updater: {err}"))
}

/// allowed_url reports whether a URL is HTTPS on a host of GitHub.
pub fn allowed_url(url: &str) -> bool {
    let Ok(parsed) = reqwest::Url::parse(url) else {
        return false;
    };
    if parsed.scheme() != "https" {
        return false;
    }
    let Some(host) = parsed.host_str() else {
        return false;
    };
    let host = host.to_ascii_lowercase();
    ALLOWED_HOSTS.contains(&host.as_str()) || host.ends_with(".githubusercontent.com")
}

/// user_agent names the app, its version and the platform.
pub fn user_agent(current_version: &str) -> String {
    format!(
        "Unison/{current_version} ({}; {})",
        std::env::consts::OS,
        std::env::consts::ARCH
    )
}

/// check reads the releases and returns the newest one that is newer than the
/// running version and carries every artifact.
pub async fn check<F>(
    http: &reqwest::Client,
    api_url: &str,
    config: &UpdaterConfig,
    current: &Version,
    manual: bool,
    asset_match: F,
) -> Result<CheckOutcome, CheckError>
where
    F: Fn(&str, &str) -> bool,
{
    let mut request = http
        .get(api_url)
        .header(reqwest::header::ACCEPT, "application/vnd.github+json")
        .header("X-GitHub-Api-Version", "2022-11-28")
        .header(
            reqwest::header::USER_AGENT,
            user_agent(&current.to_string()),
        )
        .timeout(METADATA_TIMEOUT);
    // A manual check always asks without the cached tag, so the user sees the
    // state of the channel as it is now.
    if !manual && let Some(etag) = &config.last_etag {
        request = request.header(reqwest::header::IF_NONE_MATCH, etag);
    }

    let response = request
        .send()
        .await
        .map_err(|err| CheckError::new("network", format!("cannot reach GitHub: {err}")))?;
    let status = response.status();
    let headers = response.headers().clone();
    let etag = header_text(&headers, reqwest::header::ETAG.as_str());

    if status == reqwest::StatusCode::NOT_MODIFIED {
        return Ok(CheckOutcome {
            not_modified: true,
            release: None,
            etag,
        });
    }
    if !status.is_success() {
        let remaining = header_number(&headers, "x-ratelimit-remaining");
        if (status == reqwest::StatusCode::FORBIDDEN
            || status == reqwest::StatusCode::TOO_MANY_REQUESTS)
            && (remaining == Some(0) || status == reqwest::StatusCode::TOO_MANY_REQUESTS)
        {
            let retry = header_number(&headers, "x-ratelimit-reset").map(|seconds| seconds * 1000);
            return Err(CheckError::new(
                "rate_limited",
                "GitHub limits the requests of this address",
            )
            .at(retry));
        }
        return Err(CheckError::new(
            "github_status",
            format!("GitHub answered with the status {}", status.as_u16()),
        ));
    }

    let text = response
        .text()
        .await
        .map_err(|err| CheckError::new("network", format!("cannot read the answer: {err}")))?;
    let releases: Vec<ApiRelease> = serde_json::from_str(&text).map_err(|err| {
        CheckError::new(
            "invalid_response",
            format!("the answer is not valid: {err}"),
        )
    })?;

    let release = select(&releases, config, current, &asset_match);
    Ok(CheckOutcome {
        not_modified: false,
        release,
        etag,
    })
}

/// select returns the newest release that fits the channel and the version and
/// carries the artifact, the manifest and its signature.
fn select<F>(
    releases: &[ApiRelease],
    config: &UpdaterConfig,
    current: &Version,
    asset_match: &F,
) -> Option<ReleaseInfo>
where
    F: Fn(&str, &str) -> bool,
{
    let mut best: Option<(Version, ReleaseInfo)> = None;
    for release in releases {
        if release.draft {
            continue;
        }
        let Ok(version) = Version::parse(&release.tag_name) else {
            continue;
        };
        if !config.beta_channel && (release.prerelease || version.is_prerelease()) {
            continue;
        }
        if version <= *current {
            continue;
        }
        let normalized = version.to_string();
        let Some((asset, sums, signature)) = artifacts(release, &normalized, asset_match) else {
            continue;
        };
        if !allowed_url(&asset.browser_download_url)
            || !allowed_url(&sums.browser_download_url)
            || !allowed_url(&signature.browser_download_url)
        {
            continue;
        }
        let body = truncate(&release.body.clone().unwrap_or_default(), MAX_BODY_BYTES);
        let info = ReleaseInfo {
            summary: ReleaseSummary {
                version: normalized,
                name: release
                    .name
                    .clone()
                    .unwrap_or_else(|| release.tag_name.clone()),
                body,
                prerelease: release.prerelease || version.is_prerelease(),
                published_at: release.published_at.clone().unwrap_or_default(),
                html_url: release.html_url.clone().unwrap_or_default(),
                asset_size: asset.size,
            },
            tag_name: release.tag_name.clone(),
            asset_name: asset.name.clone(),
            asset_url: asset.browser_download_url.clone(),
            checksums_url: sums.browser_download_url.clone(),
            signature_url: signature.browser_download_url.clone(),
        };
        if best.as_ref().is_none_or(|(seen, _)| version > *seen) {
            best = Some((version, info));
        }
    }
    best.map(|(_, info)| info)
}

/// artifacts returns the artifact, the manifest and the signature of a release.
fn artifacts<'a, F>(
    release: &'a ApiRelease,
    version: &str,
    asset_match: &F,
) -> Option<(&'a ApiAsset, &'a ApiAsset, &'a ApiAsset)>
where
    F: Fn(&str, &str) -> bool,
{
    let asset = release
        .assets
        .iter()
        .find(|asset| asset_match(&asset.name, version))?;
    let sums = release
        .assets
        .iter()
        .find(|asset| asset.name == "SHA256SUMS")?;
    let signature = release
        .assets
        .iter()
        .find(|asset| asset.name == "SHA256SUMS.sig")?;
    Some((asset, sums, signature))
}

/// truncate cuts a text at or before a byte limit without splitting a
/// character.
fn truncate(text: &str, limit: usize) -> String {
    if text.len() <= limit {
        return text.to_string();
    }
    let mut end = limit;
    while end > 0 && !text.is_char_boundary(end) {
        end -= 1;
    }
    text[..end].to_string()
}

/// header_text returns the value of a header.
fn header_text(headers: &reqwest::header::HeaderMap, name: &str) -> Option<String> {
    headers
        .get(name)
        .and_then(|value| value.to_str().ok())
        .map(str::to_string)
}

/// header_number returns a header as a number.
fn header_number(headers: &reqwest::header::HeaderMap, name: &str) -> Option<i64> {
    header_text(headers, name)?.trim().parse().ok()
}

/// ApiRelease is one release of the API of GitHub.
#[derive(Debug, Deserialize)]
struct ApiRelease {
    #[serde(default)]
    tag_name: String,
    name: Option<String>,
    body: Option<String>,
    published_at: Option<String>,
    #[serde(default)]
    prerelease: bool,
    #[serde(default)]
    draft: bool,
    html_url: Option<String>,
    #[serde(default)]
    assets: Vec<ApiAsset>,
}

/// ApiAsset is one artifact of a release.
#[derive(Debug, Deserialize)]
struct ApiAsset {
    name: String,
    browser_download_url: String,
    #[serde(default)]
    size: u64,
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::updater::testserver::{MockRequest, MockServer, Reply};

    /// matcher accepts the artifacts of a test.
    fn matcher(filename: &str, version: &str) -> bool {
        filename == format!("unison-v{version}-macos.dmg")
    }

    /// asset returns the JSON of one artifact.
    fn asset(version: &str, name: &str, size: u64) -> serde_json::Value {
        serde_json::json!({
            "name": name,
            "browser_download_url": format!("https://github.com/lebe-dev/unison/releases/download/v{version}/{name}"),
            "size": size,
        })
    }

    /// release returns the JSON of one release with every artifact.
    fn release(tag: &str, prerelease: bool, draft: bool) -> serde_json::Value {
        let version = tag.trim_start_matches('v');
        serde_json::json!({
            "tag_name": tag,
            "name": format!("Unison {version}"),
            "body": format!("## What is new in {version}"),
            "published_at": "2026-11-24T10:00:00Z",
            "prerelease": prerelease,
            "draft": draft,
            "html_url": format!("https://github.com/lebe-dev/unison/releases/tag/{tag}"),
            "assets": [
                asset(version, &format!("unison-v{version}-macos.dmg"), 1024),
                asset(version, "SHA256SUMS", 128),
                asset(version, "SHA256SUMS.sig", 64),
            ],
        })
    }

    /// server answers the releases of a test.
    async fn server(releases: Vec<serde_json::Value>) -> MockServer {
        let body = serde_json::to_vec(&releases).expect("serialize the releases");
        MockServer::start(move |_: &MockRequest| Reply::ok(body.clone())).await
    }

    /// check_one runs one check against a server.
    async fn check_one(
        server: &MockServer,
        config: &UpdaterConfig,
        current: &str,
        manual: bool,
    ) -> Result<CheckOutcome, CheckError> {
        check(
            &client().expect("client"),
            &server.url("/releases"),
            config,
            &Version::parse(current).expect("version"),
            manual,
            matcher,
        )
        .await
    }

    #[tokio::test]
    async fn picks_the_newest_release_of_the_channel() {
        let server = server(vec![
            release("0.3.0-beta.2", true, false),
            release("0.2.9", false, false),
            release("0.3.0", false, true),
            release("0.2.10", false, false),
        ])
        .await;

        let outcome = check_one(&server, &UpdaterConfig::default(), "0.2.5", false)
            .await
            .expect("check");
        let release = outcome.release.expect("a release");
        assert_eq!(release.summary.version, "0.2.10");
        assert_eq!(release.asset_name, "unison-v0.2.10-macos.dmg");
        assert_eq!(release.summary.asset_size, 1024);
    }

    #[tokio::test]
    async fn includes_a_pre_release_only_on_the_beta_channel() {
        let server = server(vec![release("0.3.0-beta.2", true, false)]).await;
        let stable = check_one(&server, &UpdaterConfig::default(), "0.2.5", false)
            .await
            .expect("check");
        assert!(stable.release.is_none());

        let beta = UpdaterConfig {
            beta_channel: true,
            ..UpdaterConfig::default()
        };
        let outcome = check_one(&server, &beta, "0.2.5", false)
            .await
            .expect("check");
        assert_eq!(
            outcome.release.expect("a release").summary.version,
            "0.3.0-beta.2"
        );
    }

    #[tokio::test]
    async fn skips_a_release_without_the_manifest_or_signature() {
        let mut broken = release("0.3.0", false, false);
        broken["assets"] = serde_json::json!([
            asset("0.3.0", "unison-v0.3.0-macos.dmg", 1024),
            asset("0.3.0", "SHA256SUMS", 128),
        ]);
        let server = server(vec![broken, release("0.2.9", false, false)]).await;

        let outcome = check_one(&server, &UpdaterConfig::default(), "0.2.5", false)
            .await
            .expect("check");
        assert_eq!(outcome.release.expect("a release").summary.version, "0.2.9");
    }

    #[tokio::test]
    async fn a_cached_answer_is_not_modified() {
        let server = MockServer::start(|request: &MockRequest| {
            if request.headers.contains_key(reqwest::header::IF_NONE_MATCH) {
                Reply {
                    status: 304,
                    headers: vec![("ETag".to_string(), "\"v7\"".to_string())],
                    body: Vec::new(),
                }
            } else {
                Reply::ok("[]")
            }
        })
        .await;
        let config = UpdaterConfig {
            last_etag: Some("\"v7\"".to_string()),
            ..UpdaterConfig::default()
        };

        let outcome = check_one(&server, &config, "0.2.5", false)
            .await
            .expect("check");
        assert!(outcome.not_modified);
        assert!(outcome.release.is_none());
        assert_eq!(outcome.etag.as_deref(), Some("\"v7\""));
    }

    #[tokio::test]
    async fn a_manual_check_asks_without_the_cached_tag() {
        let server = MockServer::start(|request: &MockRequest| {
            if request.headers.contains_key(reqwest::header::IF_NONE_MATCH) {
                Reply {
                    status: 304,
                    headers: Vec::new(),
                    body: Vec::new(),
                }
            } else {
                Reply::ok("[]")
            }
        })
        .await;
        let config = UpdaterConfig {
            last_etag: Some("\"v7\"".to_string()),
            ..UpdaterConfig::default()
        };

        let outcome = check_one(&server, &config, "0.2.5", true)
            .await
            .expect("check");
        assert!(!outcome.not_modified);
    }

    #[tokio::test]
    async fn reports_a_rate_limit_with_its_reset() {
        let server = MockServer::start(|_: &MockRequest| Reply {
            status: 403,
            headers: vec![
                ("X-RateLimit-Remaining".to_string(), "0".to_string()),
                ("X-RateLimit-Reset".to_string(), "1800000000".to_string()),
            ],
            body: b"API rate limit exceeded".to_vec(),
        })
        .await;

        let err = check_one(&server, &UpdaterConfig::default(), "0.2.5", false)
            .await
            .expect_err("a rate limit");
        assert_eq!(err.code, "rate_limited");
        assert_eq!(err.retry_after_unix_ms, Some(1_800_000_000_000));
    }

    #[tokio::test]
    async fn skips_a_release_whose_artifact_is_not_on_https() {
        let mut unsafe_release = release("0.3.0", false, false);
        unsafe_release["assets"][0]["browser_download_url"] =
            serde_json::json!("http://example.com/unison.dmg");
        let server = server(vec![unsafe_release]).await;

        let outcome = check_one(&server, &UpdaterConfig::default(), "0.2.5", false)
            .await
            .expect("check");
        assert!(outcome.release.is_none());
    }

    #[tokio::test]
    async fn a_redirect_to_another_host_is_refused() {
        let server = MockServer::start(|_: &MockRequest| Reply {
            status: 302,
            headers: vec![(
                "Location".to_string(),
                "https://example.com/a.dmg".to_string(),
            )],
            body: Vec::new(),
        })
        .await;

        let result = client()
            .expect("client")
            .get(server.url("/a.dmg"))
            .send()
            .await;

        assert!(result.is_err(), "a third party redirect was followed");
    }

    #[test]
    fn only_https_on_the_hosts_of_github_is_allowed() {
        assert!(allowed_url(
            "https://github.com/lebe-dev/unison/releases/download/v0.3.0/a.dmg"
        ));
        assert!(allowed_url("https://objects.githubusercontent.com/a"));
        assert!(allowed_url("https://example.githubusercontent.com/a"));
        assert!(!allowed_url("http://github.com/a"));
        assert!(!allowed_url("https://example.com/a"));
        assert!(!allowed_url("file:///tmp/a"));
        assert!(!allowed_url("not a url"));
    }

    #[test]
    fn the_changelog_is_cut_without_splitting_a_character() {
        let text = "ä".repeat(MAX_BODY_BYTES);
        let cut = truncate(&text, MAX_BODY_BYTES);
        assert!(cut.len() <= MAX_BODY_BYTES);
        assert!(text.starts_with(&cut));
    }
}
