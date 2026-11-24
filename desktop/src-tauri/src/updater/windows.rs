//! Installation of an update on Windows.
//!
//! A release ships a per-machine NSIS installer. The running app locks its own
//! binary, and the daemon needs the clipboard session and the DPAPI secrets of
//! the logged-in user, so neither may run elevated. The app therefore stops the
//! daemon, copies itself outside the installation directory and leaves. The copy
//! waits for the app to exit, runs the installer with one elevation prompt and
//! starts the updated app under the same user.

use std::ffi::OsString;
use std::path::{Path, PathBuf};

/// APPLY_ARG selects the detached helper on the command line.
const APPLY_ARG: &str = "--apply-update";
/// PARENT_ARG names the app the helper waits for.
const PARENT_ARG: &str = "--parent-pid=";
/// INSTALLER_ARG names the verified installer.
const INSTALLER_ARG: &str = "--installer=";
/// INSTALL_DIR_ARG names the directory of the installed app.
const INSTALL_DIR_ARG: &str = "--install-dir=";
/// RESTORE_ARG tells the helper whether the installer must not re-enable the
/// startup entry of a user who turned it off.
const RESTORE_ARG: &str = "--restore-autostart-off=";
/// CLEANUP_ARG names the staging directory the helper removes afterwards.
const CLEANUP_ARG: &str = "--cleanup-dir=";

/// asset_match reports whether a file of a release is the artifact of Windows.
pub fn asset_match(filename: &str, version: &str) -> bool {
    filename == format!("unison-v{version}-windows.exe")
}

/// can_self_update reports why the app cannot replace itself here.
pub fn can_self_update() -> Result<(), String> {
    if cfg!(debug_assertions) {
        return Err("in-app updates are disabled in development builds".to_string());
    }
    if !cfg!(target_arch = "x86_64") {
        return Err("in-app updates are available only in x86_64 builds".to_string());
    }
    let exe = std::env::current_exe().map_err(|err| format!("cannot find the app: {err}"))?;
    match exe.parent() {
        Some(dir) if dir.is_dir() => Ok(()),
        _ => Err("the installation directory of the app does not exist".to_string()),
    }
}

/// Options are the arguments of the detached helper.
#[derive(Debug, PartialEq, Eq)]
struct Options {
    parent_pid: u32,
    installer: PathBuf,
    install_dir: PathBuf,
    restore_autostart_off: bool,
    cleanup_dir: Option<PathBuf>,
}

impl Options {
    /// to_args formats the options as command line arguments for the detached
    /// helper.
    fn to_args(&self) -> Vec<OsString> {
        let mut args = vec![
            OsString::from(APPLY_ARG),
            OsString::from(format!("{PARENT_ARG}{}", self.parent_pid)),
            OsString::from(format!("{INSTALLER_ARG}{}", self.installer.display())),
            OsString::from(format!("{INSTALL_DIR_ARG}{}", self.install_dir.display())),
            OsString::from(format!("{RESTORE_ARG}{}", self.restore_autostart_off)),
        ];
        if let Some(dir) = &self.cleanup_dir {
            args.push(OsString::from(format!("{CLEANUP_ARG}{}", dir.display())));
        }
        args
    }
}

/// options_from reads the arguments of the detached helper.
fn options_from(args: impl IntoIterator<Item = OsString>) -> Result<Options, String> {
    let mut parent_pid = None;
    let mut installer = None;
    let mut install_dir = None;
    let mut restore_autostart_off = false;
    let mut cleanup_dir = None;
    for arg in args {
        let text = arg.to_string_lossy();
        if let Some(value) = text.strip_prefix(PARENT_ARG) {
            let pid = value
                .parse()
                .map_err(|_| format!("the parent process id {value} is not a number"))?;
            parent_pid = Some(pid);
        } else if let Some(value) = text.strip_prefix(INSTALLER_ARG).filter(|v| !v.is_empty()) {
            installer = Some(PathBuf::from(value));
        } else if let Some(value) = text.strip_prefix(INSTALL_DIR_ARG).filter(|v| !v.is_empty()) {
            install_dir = Some(PathBuf::from(value));
        } else if let Some(value) = text.strip_prefix(RESTORE_ARG) {
            restore_autostart_off = match value {
                "true" => true,
                "false" => false,
                _ => return Err(format!("{RESTORE_ARG} carries neither true nor false")),
            };
        } else if let Some(value) = text.strip_prefix(CLEANUP_ARG).filter(|v| !v.is_empty()) {
            cleanup_dir = Some(PathBuf::from(value));
        }
    }
    Ok(Options {
        parent_pid: parent_pid.ok_or_else(|| format!("{PARENT_ARG} is missing"))?,
        installer: installer.ok_or_else(|| format!("{INSTALLER_ARG} is missing"))?,
        install_dir: install_dir.ok_or_else(|| format!("{INSTALL_DIR_ARG} is missing"))?,
        restore_autostart_off,
        cleanup_dir,
    })
}

/// requested reports whether the arguments ask for the detached helper.
fn requested(args: &[OsString]) -> bool {
    args.iter().any(|arg| arg == APPLY_ARG)
}

/// nsis_parameters returns the silent parameters of the installer. NSIS takes
/// `/D=` as the last parameter and without quotes, even when the directory
/// carries a space.
fn nsis_parameters(install_dir: &Path) -> String {
    format!("/S /D={}", install_dir.display())
}

#[cfg(windows)]
mod runtime {
    use std::ffi::OsString;
    use std::os::windows::process::CommandExt;
    use std::path::{Path, PathBuf};
    use std::process::{Command, Stdio};
    use std::time::Duration;

    use tauri::AppHandle;
    use windows_sys::Win32::System::Threading::{
        CREATE_NO_WINDOW, DETACHED_PROCESS, OpenProcess, PROCESS_SYNCHRONIZE, WaitForSingleObject,
    };

    use super::{Options, nsis_parameters, options_from, requested};
    use crate::updater::InstallPhase;
    use crate::winutil::{ElevateError, OwnedHandle, run_elevated, run_system_tool};

    /// APP is the window binary inside the installation directory.
    const APP: &str = "Unison.exe";
    /// TASKKILL is the tool of the system that stops the daemon.
    const TASKKILL: &str = "taskkill.exe";
    /// HELPER_NAME is the copy of the app that waits outside the installation.
    const HELPER_NAME: &str = "unison-updater.exe";
    /// PARENT_WAIT_MS is how long the helper waits for the app to release its
    /// binary.
    const PARENT_WAIT_MS: u32 = 10_000;
    /// STOP_WAIT is how long the app waits for the daemon to close its endpoint.
    const STOP_WAIT: Duration = Duration::from_secs(5);
    /// KILL_WAIT is the pause after a forced end of the daemon.
    const KILL_WAIT: Duration = Duration::from_secs(1);
    /// CLEAN_WAIT is the pause when the endpoint of the daemon is unknown.
    const CLEAN_WAIT: Duration = Duration::from_secs(3);

    /// run_arg handles the flags of the detached helper. It returns the exit code,
    /// or None when an ordinary start runs.
    pub fn run_arg() -> Option<i32> {
        let args: Vec<OsString> = std::env::args_os().skip(1).collect();
        if !requested(&args) {
            return None;
        }
        let options = match options_from(args) {
            Ok(options) => options,
            Err(err) => {
                crate::journal::record("WARN", &format!("the updater was not started: {err}"));
                return Some(2);
            }
        };
        Some(apply(options))
    }

    /// install stops the daemon, hands the installer to the detached helper and
    /// leaves, so the installer can replace the binary of the app.
    pub async fn install(
        app: &AppHandle,
        artifact_path: &Path,
        expected_version: &str,
        endpoint: Option<&str>,
        on_phase: &(dyn Fn(InstallPhase) + Send + Sync),
    ) -> Result<(), String> {
        on_phase(InstallPhase::StoppingDaemon);
        let was_autostart_enabled =
            crate::agent::status() == crate::agent::LoginItemStatus::Enabled;
        stop_daemon(endpoint).await;

        on_phase(InstallPhase::ApplyingUpdate);
        let exe = std::env::current_exe().map_err(|err| format!("cannot find the app: {err}"))?;
        let install_dir = exe
            .parent()
            .ok_or_else(|| "the app has no directory".to_string())?;
        let staging = artifact_path
            .parent()
            .ok_or_else(|| "the installer has no staging directory".to_string())?;
        let helper = prepare_helper(app, &exe)?;

        let options = Options {
            parent_pid: std::process::id(),
            installer: artifact_path.to_path_buf(),
            install_dir: install_dir.to_path_buf(),
            restore_autostart_off: !was_autostart_enabled,
            cleanup_dir: Some(staging.to_path_buf()),
        };

        crate::journal::record(
            "INFO",
            &format!(
                "installing the update {expected_version} from {}",
                install_dir.display()
            ),
        );
        Command::new(&helper)
            .args(options.to_args())
            .creation_flags(CREATE_NO_WINDOW | DETACHED_PROCESS)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .map_err(|err| format!("cannot start the updater {}: {err}", helper.display()))?;

        on_phase(InstallPhase::Relaunching);
        app.exit(0);
        Ok(())
    }

    /// prepare_helper copies the app outside the installation directory. The copy
    /// stays in a directory of its own, so the sweeper of a later start removes it
    /// without touching the downloads.
    fn prepare_helper(app: &AppHandle, exe: &Path) -> Result<PathBuf, String> {
        let root = crate::updater::verify::staging_root(app).map_err(|err| err.message)?;
        let dir = root.join(format!("unison-updater-{}", std::process::id()));
        std::fs::create_dir_all(&dir)
            .map_err(|err| format!("cannot create {}: {err}", dir.display()))?;
        let helper = dir.join(HELPER_NAME);
        std::fs::copy(exe, &helper).map_err(|err| {
            let _ = std::fs::remove_dir_all(&dir);
            format!("cannot copy the updater to {}: {err}", helper.display())
        })?;
        Ok(helper)
    }

    /// stop_daemon stops the daemon before the installer replaces it. A clean
    /// request lets the daemon close its database first, and a daemon that does not
    /// obey is ended by force.
    async fn stop_daemon(endpoint: Option<&str>) {
        let _ = taskkill(false);
        let endpoint = endpoint
            .map(str::to_string)
            .or_else(|| crate::daemon::resolve_endpoint().ok());
        match &endpoint {
            Some(endpoint) => crate::daemon::wait_stopped(endpoint, STOP_WAIT).await,
            None => tokio::time::sleep(CLEAN_WAIT).await,
        }
        // taskkill reports the daemon that already exited as an error, which is not
        // a failure of the stop.
        let _ = taskkill(true);
        tokio::time::sleep(KILL_WAIT).await;
    }

    /// taskkill ends the daemon, with the forced end when the clean stop failed.
    fn taskkill(force: bool) -> Result<i32, String> {
        let mut args = vec!["/IM", crate::install::BINARY];
        if force {
            args.push("/F");
        }
        run_system_tool(TASKKILL, args)
    }

    /// apply runs the update in the detached helper and returns its exit code.
    fn apply(options: Options) -> i32 {
        if let Err(err) = apply_update(&options) {
            crate::journal::record("ERROR", &format!("the update was not installed: {err}"));
            relaunch(&options.install_dir);
            return 1;
        }
        0
    }

    /// apply_update waits for the app, runs the installer and starts the updated
    /// app as the current user.
    fn apply_update(options: &Options) -> Result<(), String> {
        wait_for_parent(options.parent_pid);
        let code = elevate(&options.installer, &options.install_dir)?;
        if code != 0 {
            return Err(format!("the installer exited with {code}"));
        }
        if options.restore_autostart_off
            && let Err(err) = crate::agent::set(false)
        {
            crate::journal::record(
                "WARN",
                &format!("the startup entry could not be removed: {err}"),
            );
        }
        relaunch(&options.install_dir);
        if let Some(dir) = &options.cleanup_dir {
            let _ = std::fs::remove_dir_all(dir);
        }
        Ok(())
    }

    /// wait_for_parent waits until the app releases the binary of the installation.
    fn wait_for_parent(pid: u32) {
        // SAFETY: OpenProcess returns a valid handle or null when the process
        // already exited.
        let Some(handle) = OwnedHandle::new(unsafe { OpenProcess(PROCESS_SYNCHRONIZE, 0, pid) })
        else {
            return;
        };
        // SAFETY: handle is valid and closed automatically on drop.
        unsafe { WaitForSingleObject(handle.as_raw(), PARENT_WAIT_MS) };
    }

    /// elevate runs the installer with one elevation prompt and returns its exit
    /// code.
    fn elevate(installer: &Path, install_dir: &Path) -> Result<i32, String> {
        run_elevated(installer, &nsis_parameters(install_dir)).map_err(|err| match err {
            ElevateError::Cancelled => {
                "the update was cancelled in the elevation prompt".to_string()
            }
            ElevateError::Start(code) => format!("cannot start the installer: error {code}"),
            ElevateError::NoProcessHandle => "cannot wait for the installer".to_string(),
            ElevateError::ReadExitCode => "cannot read the result of the installer".to_string(),
        })
    }

    /// relaunch starts the app of the installation directory as the current user.
    /// The helper runs without elevation, so the new app inherits the token of that
    /// user.
    fn relaunch(install_dir: &Path) {
        let app = install_dir.join(APP);
        let result = Command::new(&app)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn();
        if let Err(err) = result {
            crate::journal::record(
                "ERROR",
                &format!("the app {} did not start: {err}", app.display()),
            );
        }
    }
}

#[cfg(windows)]
pub use runtime::{install, run_arg};

#[cfg(test)]
mod tests {
    use std::ffi::OsString;

    use super::*;

    fn args(list: &[&str]) -> Vec<OsString> {
        list.iter().map(OsString::from).collect()
    }

    #[test]
    fn asset_match_takes_the_installer_of_the_release() {
        assert!(asset_match("unison-v0.3.0-windows.exe", "0.3.0"));
        assert!(asset_match(
            "unison-v0.3.0-beta.1-windows.exe",
            "0.3.0-beta.1"
        ));
        assert!(!asset_match("unison-v0.3.0-macos.dmg", "0.3.0"));
        assert!(!asset_match("unison-v0.3.0-linux.deb", "0.3.0"));
        assert!(!asset_match("unison-v0.3.0-android.apk", "0.3.0"));
        assert!(!asset_match("unison-v0.3.0-cli.zip", "0.3.0"));
        assert!(!asset_match("unison-v0.3.0-windows.exe", "0.3.1"));
    }

    #[test]
    fn nsis_parameters_put_the_directory_last_and_without_quotes() {
        let params = nsis_parameters(Path::new(r"C:\Program Files\Unison"));
        assert_eq!(params, r"/S /D=C:\Program Files\Unison");
        assert!(
            params.ends_with(r"/D=C:\Program Files\Unison"),
            "the directory is not the last parameter"
        );
        assert!(!params.contains('"'), "the directory is quoted");
    }

    #[test]
    fn requested_reads_the_helper_argument() {
        assert!(requested(&args(&[APPLY_ARG])));
        assert!(requested(&args(&["--verbose", APPLY_ARG])));
        assert!(!requested(&args(&[])));
        assert!(!requested(&args(&["--login-item=status"])));
        assert!(
            !requested(&args(&["--apply-update=1"])),
            "an argument with a value was taken for the helper"
        );
    }

    #[test]
    fn options_from_reads_the_paths_with_spaces() {
        let options = options_from(args(&[
            APPLY_ARG,
            "--parent-pid=4242",
            r"--installer=C:\Users\Some One\AppData\Local\Temp\unison-v0.3.0-windows.exe",
            r"--install-dir=C:\Program Files\Unison",
            "--restore-autostart-off=true",
            r"--cleanup-dir=C:\Users\Some One\AppData\Local\Temp\unison\updates\0.3.0",
        ]))
        .expect("read the arguments");

        assert_eq!(
            options,
            Options {
                parent_pid: 4242,
                installer: PathBuf::from(
                    r"C:\Users\Some One\AppData\Local\Temp\unison-v0.3.0-windows.exe"
                ),
                install_dir: PathBuf::from(r"C:\Program Files\Unison"),
                restore_autostart_off: true,
                cleanup_dir: Some(PathBuf::from(
                    r"C:\Users\Some One\AppData\Local\Temp\unison\updates\0.3.0"
                )),
            }
        );
    }

    #[test]
    fn options_round_trip_through_command_line_args() {
        let original = Options {
            parent_pid: 9001,
            installer: PathBuf::from(
                r"C:\Users\Some One\AppData\Local\Temp\unison-v0.3.0-windows.exe",
            ),
            install_dir: PathBuf::from(r"C:\Program Files\Unison"),
            restore_autostart_off: true,
            cleanup_dir: Some(PathBuf::from(
                r"C:\Users\Some One\AppData\Local\Temp\unison\updates\0.3.0",
            )),
        };
        let encoded = original.to_args();
        assert!(requested(&encoded));
        assert_eq!(options_from(encoded), Ok(original));
    }

    #[test]
    fn options_from_leaves_the_cleanup_absent() {
        let options = options_from(args(&[
            APPLY_ARG,
            "--parent-pid=7",
            r"--installer=C:\Temp\setup.exe",
            r"--install-dir=C:\Program Files\Unison",
            "--restore-autostart-off=false",
        ]))
        .expect("read the arguments");

        assert_eq!(options.cleanup_dir, None);
        assert!(!options.restore_autostart_off);
    }

    #[test]
    fn options_from_reports_the_missing_parts() {
        let base = [
            APPLY_ARG,
            "--parent-pid=7",
            r"--installer=C:\Temp\setup.exe",
            r"--install-dir=C:\Program Files\Unison",
        ];
        for missing in 1..base.len() {
            let mut list = base.to_vec();
            list.remove(missing);
            assert!(
                options_from(args(&list)).is_err(),
                "the missing argument {missing} was accepted"
            );
        }
    }

    #[test]
    fn options_from_refuses_empty_paths() {
        assert!(
            options_from(args(&[
                APPLY_ARG,
                "--parent-pid=7",
                "--installer=",
                r"--install-dir=C:\Program Files\Unison",
            ]))
            .is_err()
        );
        assert!(
            options_from(args(&[
                APPLY_ARG,
                "--parent-pid=7",
                r"--installer=C:\Temp\setup.exe",
                "--install-dir=",
            ]))
            .is_err()
        );
        let options = options_from(args(&[
            APPLY_ARG,
            "--parent-pid=7",
            r"--installer=C:\Temp\setup.exe",
            r"--install-dir=C:\Program Files\Unison",
            "--cleanup-dir=",
        ]))
        .expect("empty cleanup-dir is ignored");
        assert_eq!(options.cleanup_dir, None);
    }

    #[test]
    fn options_from_refuses_an_unknown_restore_value() {
        let err = options_from(args(&[
            APPLY_ARG,
            "--parent-pid=7",
            r"--installer=C:\Temp\setup.exe",
            r"--install-dir=C:\Program Files\Unison",
            "--restore-autostart-off=yes",
        ]))
        .expect_err("an unknown value was accepted");
        assert!(err.contains(RESTORE_ARG), "{err}");
    }

    #[test]
    fn options_from_refuses_a_parent_pid_that_is_no_number() {
        let err = options_from(args(&[
            APPLY_ARG,
            "--parent-pid=abc",
            r"--installer=C:\Temp\setup.exe",
            r"--install-dir=C:\Program Files\Unison",
        ]))
        .expect_err("a broken process id was accepted");
        assert!(err.contains("not a number"), "{err}");
    }

    #[test]
    fn a_development_build_cannot_update_itself() {
        assert!(can_self_update().is_err());
    }
}
