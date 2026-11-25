//! Installation of an update on Linux.
//!
//! The verified package is staged where `apt` can read it as its own user,
//! installed through PolicyKit, and the daemon is restarted in the session of
//! the user, because the package manager runs as root and cannot reach it.

use std::fs;
use std::io::ErrorKind;
use std::os::unix::fs::{DirBuilderExt, MetadataExt, OpenOptionsExt, PermissionsExt};
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::OnceLock;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use tauri::AppHandle;

use super::InstallPhase;

/// PACKAGE is the Debian package of the release.
const PACKAGE: &str = "unison";

/// STAGING_BASE is the directory where `apt` may read the package as `_apt`.
const STAGING_BASE: &str = "/tmp";

/// STAGING_ATTEMPTS bounds the tries for an unused staging name.
const STAGING_ATTEMPTS: u32 = 8;

/// WAIT_AFTER_RESTART is how long the app waits for the restarted daemon.
const WAIT_AFTER_RESTART: Duration = Duration::from_secs(10);

/// STOP_WAIT is how long the app waits for a stopped daemon without systemd.
const STOP_WAIT: Duration = Duration::from_secs(5);

/// QUIT_GRACE is the pause before the app exits, so the window receives the
/// restarting status.
const QUIT_GRACE: Duration = Duration::from_millis(300);

/// SYSTEM_BIN_DIRS are searched before PATH when resolving privileged tools.
const SYSTEM_BIN_DIRS: [&str; 4] = ["/usr/bin", "/bin", "/usr/sbin", "/sbin"];

/// asset_match reports whether a file of a release is the artifact of Linux.
pub fn asset_match(filename: &str, version: &str) -> bool {
    filename == format!("unison-v{version}-linux.deb")
}

/// can_self_update reports why the app cannot replace itself here.
pub fn can_self_update() -> Result<(), String> {
    check_self_update(
        cfg!(debug_assertions),
        cfg!(target_arch = "x86_64"),
        is_package_installed(),
        find_executable("pkexec").is_some(),
        find_executable("apt-get").is_some(),
    )
}

/// check_self_update evaluates the preconditions for an in-app update.
fn check_self_update(
    debug: bool,
    x86_64: bool,
    installed: bool,
    has_pkexec: bool,
    has_apt_get: bool,
) -> Result<(), String> {
    if debug {
        return Err("in-app updates are disabled in development builds".to_string());
    }
    if !x86_64 {
        return Err("in-app updates are only supported on 64-bit x86 Linux".to_string());
    }
    if !installed {
        return Err(
            "in-app updates are only supported for Debian package installations".to_string(),
        );
    }
    if !has_pkexec {
        return Err(
            "pkexec is not available; download the .deb release and install it with sudo apt install"
                .to_string(),
        );
    }
    if !has_apt_get {
        return Err(
            "apt-get is not available; download the .deb release and install it with sudo apt install"
                .to_string(),
        );
    }
    Ok(())
}

/// run_arg handles the flags the installer starts the app with.
pub fn run_arg() -> Option<i32> {
    None
}

/// install replaces the app with the downloaded artifact.
pub async fn install(
    app: &AppHandle,
    artifact_path: &Path,
    expected_version: &str,
    endpoint: Option<&str>,
    on_phase: &(dyn Fn(InstallPhase) + Send + Sync),
) -> Result<(), String> {
    can_self_update()?;

    // The running binary keeps its path until the package replaces the file on
    // disk, after which the path of the running process ends in " (deleted)".
    let current_exe = std::env::current_exe()
        .and_then(|path| path.canonicalize())
        .map_err(|err| format!("cannot find the running application: {err}"))?;

    {
        on_phase(InstallPhase::Preparing);
        let staging = stage(Path::new(STAGING_BASE), artifact_path, expected_version)?;

        on_phase(InstallPhase::ApplyingUpdate);
        run_apt(staging.package()).await?;
    }
    cleanup_download(artifact_path);

    on_phase(InstallPhase::RestartingServices);
    restart_daemon(endpoint).await;

    on_phase(InstallPhase::Relaunching);
    relaunch(&current_exe)?;

    quit(app);
    Ok(())
}

/// is_package_installed caches whether dpkg knows the installed package of the
/// app, so snapshot queries do not spawn `dpkg-query` repeatedly.
fn is_package_installed() -> bool {
    static INSTALLED: OnceLock<bool> = OnceLock::new();
    *INSTALLED.get_or_init(package_installed)
}

/// package_installed reports whether dpkg knows an installed package of the
/// app, which is how the app recognizes an installation it may replace.
fn package_installed() -> bool {
    let output = Command::new("dpkg-query")
        .args(["-W", "-f=${Status}", PACKAGE])
        .stdin(Stdio::null())
        .output();
    match output {
        Ok(output) => installed_status(
            &String::from_utf8_lossy(&output.stdout),
            output.status.success(),
        ),
        Err(_) => false,
    }
}

/// installed_status reads the output of `dpkg-query -W -f=${Status}`. A
/// package that is removed but keeps its configuration files is not installed.
fn installed_status(stdout: &str, success: bool) -> bool {
    success && stdout.trim() == "install ok installed"
}

/// find_executable returns the first executable of the name in the system
/// directories or in PATH.
fn find_executable(name: &str) -> Option<PathBuf> {
    let path_env = std::env::var_os("PATH");
    let dirs = SYSTEM_BIN_DIRS
        .iter()
        .map(PathBuf::from)
        .chain(path_env.iter().flat_map(std::env::split_paths));
    find_executable_in(name, dirs)
}

/// find_executable_in returns the first executable of the name in the given
/// directories.
fn find_executable_in(name: &str, dirs: impl IntoIterator<Item = PathBuf>) -> Option<PathBuf> {
    dirs.into_iter()
        .map(|dir| dir.join(name))
        .find(|path| is_executable(path))
}

/// is_executable reports whether a path is a regular file with an execute bit.
fn is_executable(path: &Path) -> bool {
    fs::metadata(path)
        .map(|meta| meta.is_file() && meta.permissions().mode() & 0o111 != 0)
        .unwrap_or(false)
}

/// Staging is a private copy of the package that `apt` may read. The directory
/// is removed when the copy is dropped, also after a failed installation.
struct Staging {
    dir: PathBuf,
    package: PathBuf,
}

impl Staging {
    /// package returns the staged package.
    fn package(&self) -> PathBuf {
        self.package.clone()
    }
}

impl Drop for Staging {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.dir);
    }
}

/// stage copies the artifact into a directory of `0755` that is owned by the
/// current user and readable by `_apt`, which drops the privileges of `apt`.
fn stage(base: &Path, artifact: &Path, expected_version: &str) -> Result<Staging, String> {
    let name = artifact
        .file_name()
        .and_then(|name| name.to_str())
        .ok_or_else(|| "the update package has no file name".to_string())?;
    if !asset_match(name, expected_version) {
        return Err(format!(
            "{name} is not the package of version {expected_version}"
        ));
    }

    let dir = create_staging_dir(base)?;
    let package = dir.join(name);
    let staging = Staging { dir, package };
    copy_package(artifact, &staging.package)?;
    Ok(staging)
}

/// create_staging_dir makes an empty directory that no other process could
/// have prepared, and refuses one that is not a directory of this user.
fn create_staging_dir(base: &Path) -> Result<PathBuf, String> {
    // SAFETY: geteuid has no preconditions and always returns the effective uid.
    let uid = unsafe { libc::geteuid() };
    for _ in 0..STAGING_ATTEMPTS {
        let path = base.join(format!("unison-update-{}-{}", std::process::id(), nonce()));
        match fs::DirBuilder::new().mode(0o755).create(&path) {
            Ok(()) => {}
            Err(err) if err.kind() == ErrorKind::AlreadyExists => continue,
            Err(err) => {
                return Err(format!(
                    "cannot create the staging directory {}: {err}",
                    path.display()
                ));
            }
        }
        if !safe_directory(&path, uid) {
            let _ = fs::remove_dir_all(&path);
            return Err(
                "the staging directory does not belong to the user; nothing was changed"
                    .to_string(),
            );
        }
        if let Err(err) = fs::set_permissions(&path, fs::Permissions::from_mode(0o755)) {
            let _ = fs::remove_dir_all(&path);
            return Err(format!("cannot open the staging directory for apt: {err}"));
        }
        return Ok(path);
    }
    Err("cannot find an unused staging directory".to_string())
}

/// safe_directory reports whether a created path is a directory of the user and
/// not a symbolic link.
fn safe_directory(path: &Path, uid: u32) -> bool {
    fs::symlink_metadata(path)
        .map(|meta| meta.file_type().is_dir() && meta.uid() == uid)
        .unwrap_or(false)
}

/// copy_package writes the artifact as a readable regular file.
fn copy_package(source: &Path, target: &Path) -> Result<(), String> {
    let mut input = fs::File::open(source)
        .map_err(|err| format!("cannot read the downloaded package: {err}"))?;
    let mut output = fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o644)
        .open(target)
        .map_err(|err| format!("cannot stage the package for apt: {err}"))?;
    std::io::copy(&mut input, &mut output)
        .map_err(|err| format!("cannot copy the package for apt: {err}"))?;
    output
        .set_permissions(fs::Permissions::from_mode(0o644))
        .map_err(|err| format!("cannot open the staged package for apt: {err}"))?;
    output
        .sync_all()
        .map_err(|err| format!("cannot flush the staged package: {err}"))?;
    Ok(())
}

/// nonce returns a value that no earlier staging name of this process used.
fn nonce() -> String {
    static NEXT: AtomicU64 = AtomicU64::new(0);
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|elapsed| elapsed.as_nanos())
        .unwrap_or_default();
    format!("{nanos:x}-{}", NEXT.fetch_add(1, Ordering::Relaxed))
}

/// run_apt installs the staged package through PolicyKit. The prompt belongs to
/// the user, and the package manager resolves any new dependency itself.
async fn run_apt(package: PathBuf) -> Result<(), String> {
    let pkexec = find_executable("pkexec")
        .ok_or_else(|| "pkexec is not available; nothing was changed".to_string())?;
    let apt_get = find_executable("apt-get")
        .ok_or_else(|| "apt-get is not available; nothing was changed".to_string())?;

    let output = tokio::task::spawn_blocking(move || {
        Command::new(pkexec)
            .arg(apt_get)
            .args([
                "install",
                "-y",
                "-o",
                "Dpkg::Options::=--force-confdef",
                "-o",
                "Dpkg::Options::=--force-confold",
            ])
            .arg(&package)
            .stdin(Stdio::null())
            .output()
    })
    .await
    .map_err(|err| format!("the installation of the package did not finish: {err}"))?
    .map_err(|err| format!("cannot run pkexec: {err}"))?;

    let stderr = String::from_utf8_lossy(&output.stderr);
    match policykit_outcome(output.status.code(), &stderr) {
        Ok(()) => {
            crate::journal::record("INFO", "the update package was installed");
            Ok(())
        }
        Err(failure) => {
            let (level, message) = failure_log(&failure, &stderr);
            crate::journal::record(level, &message);
            Err(failure.message())
        }
    }
}

/// InstallFailure is what PolicyKit or the package manager reported.
#[derive(Clone, Debug, PartialEq, Eq)]
enum InstallFailure {
    /// The user dismissed the authentication prompt.
    Cancelled,
    /// The authentication failed or the user is not authorized.
    Authorization,
    /// The package manager failed with the line of its error.
    Failed(String),
}

impl InstallFailure {
    /// message returns the text of the dialog of the window.
    fn message(&self) -> String {
        match self {
            Self::Cancelled => "Authentication was cancelled; nothing was changed".to_string(),
            Self::Authorization => "Authorization failed; the update was not installed".to_string(),
            Self::Failed(line) => line.clone(),
        }
    }
}

/// failure_log builds the journal level and message for a failed installation.
fn failure_log(failure: &InstallFailure, stderr: &str) -> (&'static str, String) {
    if *failure == InstallFailure::Cancelled {
        return ("INFO", "the update installation was cancelled".to_string());
    }
    let details = stderr.trim();
    let reason = if details.is_empty() {
        failure.message()
    } else {
        details.to_string()
    };
    (
        "ERROR",
        format!("the update package was not installed: {reason}"),
    )
}

/// policykit_outcome maps the exit code of pkexec to the result of the
/// installation. pkexec uses 126 for a dismissed prompt and 127 for a failed
/// authentication.
fn policykit_outcome(code: Option<i32>, stderr: &str) -> Result<(), InstallFailure> {
    match code {
        Some(0) => Ok(()),
        Some(126) => Err(InstallFailure::Cancelled),
        Some(127) => Err(InstallFailure::Authorization),
        _ => Err(InstallFailure::Failed(error_summary(stderr))),
    }
}

/// error_summary returns the line of stderr that explains a failed run, such as
/// a held lock of the package manager or a broken package.
fn error_summary(stderr: &str) -> String {
    let lines: Vec<&str> = stderr
        .lines()
        .map(str::trim)
        .filter(|line| !line.is_empty())
        .collect();
    lines
        .iter()
        .find(|line| line.starts_with("E: "))
        .or_else(|| lines.iter().find(|line| line.starts_with("dpkg: error")))
        .or_else(|| lines.last())
        .map(|line| (*line).to_string())
        .unwrap_or_else(|| "the package manager failed without a message".to_string())
}

/// restart_daemon reloads and restarts the user unit of the daemon. A session
/// without a user manager gets the daemon of the app stopped instead, and the
/// relaunched app starts the new binary.
async fn restart_daemon(endpoint: Option<&str>) {
    let endpoint = endpoint
        .map(str::to_string)
        .or_else(|| crate::daemon::resolve_endpoint().ok());

    let restarted = tokio::task::spawn_blocking(systemctl_restart).await;
    let failure = match restarted {
        Ok(Ok(())) => None,
        Ok(Err(err)) => Some(err),
        Err(err) => Some(format!("the restart of the daemon did not finish: {err}")),
    };

    if let Some(err) = failure {
        crate::journal::record(
            "WARN",
            &format!("cannot restart the daemon through systemd: {err}"),
        );
        crate::daemon::stop_started();
        if let Some(endpoint) = endpoint.as_deref() {
            crate::daemon::wait_stopped(endpoint, STOP_WAIT).await;
        }
        return;
    }

    let Some(endpoint) = endpoint.as_deref() else {
        return;
    };
    let started =
        tokio::time::timeout(WAIT_AFTER_RESTART, crate::daemon::ensure_running(endpoint)).await;
    if !matches!(started, Ok(Ok(()))) {
        crate::journal::record(
            "WARN",
            &format!("the daemon did not answer on {endpoint} after the update"),
        );
    }
}

/// systemctl_restart asks the user manager of the session to load the unit of
/// the new package and restart the daemon.
fn systemctl_restart() -> Result<(), String> {
    systemctl_user(&["daemon-reload"])?;
    systemctl_user(&["restart", crate::agent::UNIT])
}

/// systemctl_user runs one command of the user manager of the session.
fn systemctl_user(args: &[&str]) -> Result<(), String> {
    let output = Command::new("systemctl")
        .arg("--user")
        .args(args)
        .stdin(Stdio::null())
        .output()
        .map_err(|err| format!("cannot run systemctl: {err}"))?;
    if output.status.success() {
        return Ok(());
    }
    let details = String::from_utf8_lossy(&output.stderr);
    Err(format!(
        "systemctl --user {} failed: {}",
        args.join(" "),
        details.trim()
    ))
}

/// relaunch starts the installed application again with the environment of the
/// session, so the new window finds the display, the bus and the runtime
/// directory of the user.
fn relaunch(exe: &Path) -> Result<(), String> {
    Command::new(exe)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .map(|_| ())
        .map_err(|err| format!("cannot start the updated application: {err}"))
}

/// quit exits the app after the window receives the last status update.
fn quit(app: &AppHandle) {
    let app = app.clone();
    std::thread::spawn(move || {
        std::thread::sleep(QUIT_GRACE);
        app.exit(0);
    });
}

/// cleanup_download removes the directory of the downloaded package. The staged
/// copy is removed by its guard.
fn cleanup_download(artifact: &Path) {
    let Some(dir) = artifact.parent() else {
        return;
    };
    let is_update_dir = dir
        .parent()
        .and_then(Path::file_name)
        .is_some_and(|name| name == "updates");
    if is_update_dir {
        let _ = fs::remove_dir_all(dir);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::testutil::{TempDir, set_mode};

    #[test]
    fn the_linux_package_is_matched() {
        assert!(asset_match("unison-v0.3.0-linux.deb", "0.3.0"));
        assert!(asset_match(
            "unison-v0.3.0-beta.1-linux.deb",
            "0.3.0-beta.1"
        ));
        assert!(!asset_match("unison-v0.3.0-macos.dmg", "0.3.0"));
        assert!(!asset_match("unison-v0.3.0-windows.exe", "0.3.0"));
        assert!(!asset_match("unison-v0.3.0-android.apk", "0.3.0"));
        assert!(!asset_match("unison-v0.3.0-linux.tar.gz", "0.3.0"));
    }

    #[test]
    fn self_update_checks_every_precondition() {
        assert!(check_self_update(false, true, true, true, true).is_ok());
        assert!(
            check_self_update(true, true, true, true, true)
                .unwrap_err()
                .contains("development builds")
        );
        assert!(
            check_self_update(false, false, true, true, true)
                .unwrap_err()
                .contains("64-bit x86")
        );
        assert!(
            check_self_update(false, true, false, true, true)
                .unwrap_err()
                .contains("Debian package")
        );
        assert!(
            check_self_update(false, true, true, false, true)
                .unwrap_err()
                .contains("pkexec")
        );
        assert!(
            check_self_update(false, true, true, true, false)
                .unwrap_err()
                .contains("apt-get")
        );
    }

    #[test]
    fn only_an_installed_package_is_installed() {
        assert!(installed_status("install ok installed", true));
        assert!(installed_status(" install ok installed\n", true));
        assert!(!installed_status("deinstall ok config-files", true));
        assert!(!installed_status("", true));
        assert!(!installed_status("install ok installed", false));
    }

    #[test]
    fn find_executable_prefers_the_first_executable_file() {
        let first = TempDir::new();
        let second = TempDir::new();

        let non_exec = first.join("tool");
        fs::write(&non_exec, b"#!/bin/sh\n").expect("write non-exec file");
        set_mode(&non_exec, 0o644);

        let exec = second.join("tool");
        fs::write(&exec, b"#!/bin/sh\n").expect("write exec file");
        set_mode(&exec, 0o755);

        let found = find_executable_in(
            "tool",
            [first.path().to_path_buf(), second.path().to_path_buf()],
        );
        assert_eq!(found, Some(exec));
        assert_eq!(
            find_executable_in("missing", [first.path().to_path_buf()]),
            None
        );
    }

    #[test]
    fn policykit_codes_explain_the_result() {
        assert_eq!(policykit_outcome(Some(0), ""), Ok(()));
        assert_eq!(
            policykit_outcome(Some(126), ""),
            Err(InstallFailure::Cancelled)
        );
        assert_eq!(
            policykit_outcome(Some(127), ""),
            Err(InstallFailure::Authorization)
        );
        assert_eq!(
            policykit_outcome(None, ""),
            Err(InstallFailure::Failed(
                "the package manager failed without a message".to_string()
            ))
        );
        assert_eq!(
            InstallFailure::Cancelled.message(),
            "Authentication was cancelled; nothing was changed"
        );
    }

    #[test]
    fn failure_log_distinguishes_cancellation_and_errors() {
        let (level, msg) = failure_log(&InstallFailure::Cancelled, "dismissed");
        assert_eq!(level, "INFO");
        assert!(msg.contains("cancelled"));

        let (level, msg) = failure_log(&InstallFailure::Authorization, "");
        assert_eq!(level, "ERROR");
        assert!(msg.contains("Authorization failed"));

        let (level, msg) = failure_log(
            &InstallFailure::Failed("E: lock".to_string()),
            "E: lock held\n",
        );
        assert_eq!(level, "ERROR");
        assert!(msg.ends_with("E: lock held"));
    }

    #[test]
    fn a_failed_installation_keeps_its_error_line() {
        let stderr = "\
Waiting for cache lock: Could not get lock /var/lib/dpkg/lock-frontend
E: Could not get lock /var/lib/dpkg/lock-frontend. It is held by process 1234
dpkg: error processing package unison (--install)
";
        assert_eq!(
            error_summary(stderr),
            "E: Could not get lock /var/lib/dpkg/lock-frontend. It is held by process 1234"
        );

        let stderr = "dpkg: error processing archive unison.deb (--install): corrupted\n";
        assert_eq!(
            error_summary(stderr),
            "dpkg: error processing archive unison.deb (--install): corrupted"
        );

        assert_eq!(
            error_summary(""),
            "the package manager failed without a message"
        );
    }

    #[test]
    fn a_staged_package_is_readable_by_apt_and_removed() {
        let base = TempDir::new();
        let source = TempDir::new();
        let artifact = source.join("unison-v0.3.0-linux.deb");
        fs::write(&artifact, b"package").expect("write the artifact");

        let (dir, package) = {
            let staged = stage(base.path(), &artifact, "0.3.0").expect("stage the package");
            let dir = staged.dir.clone();
            let package = staged.package();
            let dir_meta = fs::metadata(&dir).expect("read the staging directory");
            assert_eq!(dir_meta.permissions().mode() & 0o777, 0o755);
            assert!(dir_meta.is_dir());
            // SAFETY: geteuid has no preconditions and always returns the effective uid.
            assert_eq!(dir_meta.uid(), unsafe { libc::geteuid() });
            assert_eq!(package.file_name().unwrap(), "unison-v0.3.0-linux.deb");
            assert_eq!(
                fs::metadata(&package)
                    .expect("read the staged package")
                    .permissions()
                    .mode()
                    & 0o777,
                0o644
            );
            assert_eq!(
                fs::read(&package).expect("read the staged package"),
                b"package"
            );
            (dir, package)
        };

        assert!(!dir.exists());
        assert!(!package.exists());
    }

    #[test]
    fn a_failed_stage_leaves_no_directory() {
        let base = TempDir::new();
        let source = TempDir::new();
        let artifact = source.join("unison-v0.3.0-linux.deb");
        fs::write(&artifact, b"package").expect("write the artifact");

        assert!(stage(base.path(), &artifact, "0.4.0").is_err());
        assert_eq!(
            fs::read_dir(base.path())
                .expect("read the base directory")
                .count(),
            0
        );
    }

    #[test]
    fn cleanup_download_removes_only_update_directories() {
        let root = TempDir::new();
        let update_dir = root.join("updates/0.3.0");
        fs::create_dir_all(&update_dir).expect("create the update directory");
        let artifact = update_dir.join("unison-v0.3.0-linux.deb");
        fs::write(&artifact, b"package").expect("write the artifact");

        cleanup_download(&artifact);
        assert!(!update_dir.exists());

        let other_dir = root.join("other/0.3.0");
        fs::create_dir_all(&other_dir).expect("create the other directory");
        let other_artifact = other_dir.join("unison-v0.3.0-linux.deb");
        fs::write(&other_artifact, b"package").expect("write the other artifact");

        cleanup_download(&other_artifact);
        assert!(other_dir.exists());
    }
}
