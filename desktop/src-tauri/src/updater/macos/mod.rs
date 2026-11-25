//! Installation of an update on macOS.
//!
//! The verified disk image is mounted, the bundle inside it is checked and
//! copied next to the installed app. A running bundle cannot replace itself, so
//! the swap happens in a detached copy of this binary: it waits for the app to
//! exit, renames the bundles, refreshes the registration of Launch Services,
//! registers the login item again and opens the updated app.

mod bundle;
mod helper;

use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::process::{Command, Output, Stdio};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use tauri::AppHandle;

use super::InstallPhase;
use bundle::StagedBundle;
use helper::ApplyRequest;

/// Process listing utility used to locate lingering daemon processes.
const PS: &str = "/bin/ps";

/// Layout constants of the application bundle.
const BUNDLE_NAME: &str = "Unison.app";
const INFO_PLIST: &str = "Contents/Info.plist";
const DAEMON_EXECUTABLE: &str = "Contents/MacOS/unisond";
const LOGIN_ITEM: &str = "Contents/Library/LaunchAgents/app.unison.unisond.plist";

/// Timeouts for stopping the daemon before replacing the bundle.
const DAEMON_STOP_TIMEOUT: Duration = Duration::from_secs(5);
const DAEMON_KILL_TIMEOUT: Duration = Duration::from_secs(3);
const PROBE_TIMEOUT: Duration = Duration::from_millis(500);

/// Pause before the app exits, so the window keeps its last status.
const QUIT_GRACE: Duration = Duration::from_millis(300);

/// asset_match reports whether a file of a release is the artifact of macOS.
pub fn asset_match(filename: &str, version: &str) -> bool {
    filename == format!("unison-v{version}-macos.dmg")
}

/// can_self_update reports why the app cannot replace itself here.
pub fn can_self_update() -> Result<(), String> {
    bundle::current_bundle().map(|_| ())
}

/// run_arg handles the flags the installer starts the app with.
pub fn run_arg() -> Option<i32> {
    helper::run_arg()
}

/// install replaces the app with the downloaded artifact.
pub async fn install(
    app: &AppHandle,
    artifact_path: &Path,
    expected_version: &str,
    endpoint: Option<&str>,
    on_phase: &(dyn Fn(InstallPhase) + Send + Sync),
) -> Result<(), String> {
    let target = bundle::current_bundle()?;
    on_phase(InstallPhase::Preparing);

    let root = super::verify::staging_root(app).map_err(|err| err.message)?;
    let nonce = nonce();
    let mount_dir = root.join(format!("mnt-{}-{nonce}", std::process::id()));

    let staged = stage_blocking(
        artifact_path.to_path_buf(),
        target.clone(),
        expected_version.to_string(),
        mount_dir,
    )
    .await?;

    on_phase(InstallPhase::StoppingDaemon);
    let endpoint = endpoint
        .map(str::to_string)
        .or_else(|| crate::daemon::resolve_endpoint().ok());
    let login_item = crate::agent::status() == crate::agent::LoginItemStatus::Enabled;
    if let Err(err) = stop_daemon(endpoint.as_deref(), &target, login_item).await {
        if login_item {
            let _ = crate::agent::set(true);
        }
        return Err(err);
    }

    on_phase(InstallPhase::ApplyingUpdate);
    let request = ApplyRequest {
        parent_pid: std::process::id(),
        target,
        staged: staged.path().to_path_buf(),
        re_register_login_item: login_item,
        cleanup_dir: root.join(format!("apply-{}-{nonce}", std::process::id())),
    };
    if let Err(err) = helper::spawn(&request) {
        if login_item {
            let _ = crate::agent::set(true);
        }
        if let Some(endpoint) = endpoint.as_deref() {
            let _ = crate::daemon::ensure_running(endpoint).await;
        }
        return Err(err);
    }
    staged.disarm();

    on_phase(InstallPhase::Relaunching);
    quit(app);
    Ok(())
}

/// stage_blocking mounts the disk image, validates and stages the bundle on a
/// blocking worker thread so the async runtime stays responsive.
async fn stage_blocking(
    artifact: PathBuf,
    target: PathBuf,
    expected_version: String,
    mount_dir: PathBuf,
) -> Result<StagedBundle, String> {
    tokio::task::spawn_blocking(move || {
        bundle::prepare_staged(&artifact, &target, &expected_version, &mount_dir)
    })
    .await
    .map_err(|err| format!("staging the update did not finish: {err}"))?
}

/// stop_daemon stops the daemon of the login item and of the app, so nothing
/// writes the store of the old bundle during the swap.
async fn stop_daemon(
    endpoint: Option<&str>,
    target: &Path,
    login_item: bool,
) -> Result<(), String> {
    if login_item {
        crate::agent::set(false)
            .map_err(|err| format!("cannot stop the login item of the daemon: {err}"))?;
    }
    crate::daemon::stop_started();
    let Some(endpoint) = endpoint else {
        return Ok(());
    };
    crate::daemon::wait_stopped(endpoint, DAEMON_STOP_TIMEOUT).await;
    if !answers(endpoint).await {
        return Ok(());
    }
    terminate(&target.join(DAEMON_EXECUTABLE));
    crate::daemon::wait_stopped(endpoint, DAEMON_KILL_TIMEOUT).await;
    if answers(endpoint).await {
        return Err("the daemon is still running, so the app cannot be replaced".to_string());
    }
    Ok(())
}

/// answers reports whether a process serves the endpoint.
async fn answers(endpoint: &str) -> bool {
    matches!(
        tokio::time::timeout(PROBE_TIMEOUT, crate::proxy::connect(endpoint)).await,
        Ok(Ok(_))
    )
}

/// terminate asks the daemon of the old bundle to exit. A daemon that another
/// copy of the app started is left alone.
fn terminate(daemon: &Path) {
    let Ok(output) = Command::new(PS)
        .args(["-axo", "pid=,comm="])
        .stdin(Stdio::null())
        .output()
    else {
        return;
    };
    let canonical = fs::canonicalize(daemon).unwrap_or_else(|_| daemon.to_path_buf());
    let list = String::from_utf8_lossy(&output.stdout);
    for pid in daemon_pids(&list, daemon, &canonical) {
        // SAFETY: the signal asks a process of this user to exit.
        let _ = unsafe { libc::kill(pid as libc::pid_t, libc::SIGTERM) };
    }
}

/// daemon_pids returns the processes that run the given executable.
fn daemon_pids(list: &str, daemon: &Path, canonical: &Path) -> Vec<u32> {
    list.lines()
        .filter_map(|line| {
            let (pid, command) = line.trim_start().split_once(char::is_whitespace)?;
            let pid: u32 = pid.parse().ok()?;
            let command = Path::new(command.trim_end());
            (command == daemon || command == canonical).then_some(pid)
        })
        .collect()
}

/// quit ends the app after the window showed the last phase.
fn quit(app: &AppHandle) {
    let app = app.clone();
    std::thread::spawn(move || {
        std::thread::sleep(QUIT_GRACE);
        app.exit(0);
    });
}

/// sibling returns a hidden neighbour of a bundle, such as the staged copy or
/// the backup of the swap.
pub(super) fn sibling(bundle: &Path, tag: &str) -> PathBuf {
    let name = bundle
        .file_name()
        .map(|name| name.to_string_lossy().into_owned())
        .unwrap_or_else(|| BUNDLE_NAME.to_string());
    bundle.with_file_name(format!(".{name}.{tag}.{}", std::process::id()))
}

/// create_private creates a directory only the user can reach.
pub(super) fn create_private(dir: &Path) -> Result<(), String> {
    fs::create_dir_all(dir).map_err(|err| format!("cannot create the staging directory: {err}"))?;
    fs::set_permissions(dir, fs::Permissions::from_mode(0o700))
        .map_err(|err| format!("cannot restrict the staging directory: {err}"))?;
    Ok(())
}

/// nonce returns the number that keeps the directories of one run apart.
fn nonce() -> u128 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|elapsed| elapsed.as_nanos())
        .unwrap_or_default()
}

/// details returns the error output of a failed command.
pub(super) fn details(output: &Output) -> String {
    let text = String::from_utf8_lossy(&output.stderr).trim().to_string();
    if text.is_empty() {
        format!("the command exited with {}", output.status)
    } else {
        text
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_release_of_macos_matches_its_artifact() {
        assert!(asset_match("unison-v0.3.0-macos.dmg", "0.3.0"));
        assert!(asset_match(
            "unison-v0.3.0-beta.1-macos.dmg",
            "0.3.0-beta.1"
        ));
        for other in [
            "unison-v0.3.0-windows.exe",
            "unison-v0.3.0-linux.deb",
            "unison-v0.3.0-android.apk",
            "unison-v0.3.0-cli.tar.gz",
        ] {
            assert!(!asset_match(other, "0.3.0"), "{other}");
        }
        assert!(!asset_match("unison-v0.3.1-macos.dmg", "0.3.0"));
        assert!(!asset_match("Unison-v0.3.0-macos.dmg", "0.3.0"));
    }

    #[test]
    fn only_the_daemon_of_the_target_bundle_is_terminated() {
        let daemon = Path::new("/Applications/Unison.app/Contents/MacOS/unisond");
        let list = "  10 /Applications/Unison.app/Contents/MacOS/unisond\n  11 /Users/someone/pro/unison/bin/unisond\n  12 /usr/bin/unisond\n  13 /Applications/Unison.app/Contents/MacOS/unisond\n";
        assert_eq!(daemon_pids(list, daemon, daemon), [10, 13]);
        assert!(daemon_pids("  not a line\n", daemon, daemon).is_empty());
    }

    #[test]
    fn a_development_build_does_not_update_itself() {
        assert_eq!(
            can_self_update().expect_err("a development build replaced itself"),
            "in-app updates are disabled in development builds"
        );
    }
}
