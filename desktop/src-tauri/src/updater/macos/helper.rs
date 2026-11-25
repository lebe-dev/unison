//! Detached helper process that swaps the application bundle on macOS.

use std::fs;
use std::io::Write;
use std::os::unix::fs::PermissionsExt;
use std::os::unix::process::CommandExt;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

use super::bundle::main_executable;
use super::{create_private, details, sibling};

/// System utilities used to register and relaunch the updated bundle.
const OPEN: &str = "/usr/bin/open";
const LSREGISTER: &str = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister";

/// Command-line flags of the detached update helper.
const APPLY_ARG: &str = "--apply-update";
const PARENT_PID_ARG: &str = "--parent-pid=";
const TARGET_ARG: &str = "--target=";
const STAGED_ARG: &str = "--staged=";
const RE_REGISTER_ARG: &str = "--re-register-login-item=";
const CLEANUP_ARG: &str = "--cleanup-dir=";

/// How long the helper waits for the running app to exit before swapping.
const PARENT_EXIT_TIMEOUT: Duration = Duration::from_secs(10);
const PARENT_POLL: Duration = Duration::from_millis(100);

/// ApplyRequest is everything the detached helper needs for the swap.
#[derive(Clone, Debug, PartialEq, Eq)]
pub(super) struct ApplyRequest {
    pub parent_pid: u32,
    pub target: PathBuf,
    pub staged: PathBuf,
    pub re_register_login_item: bool,
    pub cleanup_dir: PathBuf,
}

impl ApplyRequest {
    /// to_args formats the request as command-line arguments for the helper.
    fn to_args(&self) -> Vec<String> {
        vec![
            APPLY_ARG.to_string(),
            format!("{PARENT_PID_ARG}{}", self.parent_pid),
            format!("{TARGET_ARG}{}", self.target.display()),
            format!("{STAGED_ARG}{}", self.staged.display()),
            format!("{RE_REGISTER_ARG}{}", self.re_register_login_item),
            format!("{CLEANUP_ARG}{}", self.cleanup_dir.display()),
        ]
    }
}

/// run_arg handles the flags the installer starts the app with.
pub(super) fn run_arg() -> Option<i32> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let request = match parse_apply_args(&args)? {
        Ok(request) => request,
        Err(err) => {
            eprintln!("unison: {err}");
            return Some(2);
        }
    };
    Some(apply(&request))
}

/// spawn starts the detached copy of this binary that swaps the bundle.
pub(super) fn spawn(request: &ApplyRequest) -> Result<(), String> {
    create_private(&request.cleanup_dir)?;
    if let Err(err) = spawn_in(request) {
        let _ = fs::remove_dir_all(&request.cleanup_dir);
        return Err(err);
    }
    Ok(())
}

/// spawn_in copies the running binary into the cleanup directory and launches
/// it in a new session.
fn spawn_in(request: &ApplyRequest) -> Result<(), String> {
    let helper = request.cleanup_dir.join("unison-updater");
    let executable =
        std::env::current_exe().map_err(|err| format!("cannot find the running app: {err}"))?;
    fs::copy(&executable, &helper)
        .map_err(|err| format!("cannot copy the update helper: {err}"))?;
    fs::set_permissions(&helper, fs::Permissions::from_mode(0o700))
        .map_err(|err| format!("cannot make the update helper executable: {err}"))?;
    let mut command = Command::new(&helper);
    command
        .args(request.to_args())
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    // SAFETY: setsid only changes the session of the child before it runs, so
    // the helper survives the exit of the app.
    unsafe {
        command.pre_exec(|| {
            if libc::setsid() == -1 {
                return Err(std::io::Error::last_os_error());
            }
            Ok(())
        });
    }
    command
        .spawn()
        .map(|_| ())
        .map_err(|err| format!("cannot start the update helper: {err}"))
}

/// parse_apply_args returns the request of a helper run, or None when the
/// arguments ask for the regular start.
fn parse_apply_args(args: &[String]) -> Option<Result<ApplyRequest, String>> {
    if !args.iter().any(|arg| arg == APPLY_ARG) {
        return None;
    }
    Some(read_apply_args(args))
}

/// read_apply_args reads the options that follow `--apply-update`.
fn read_apply_args(args: &[String]) -> Result<ApplyRequest, String> {
    let value = |prefix: &str| -> Result<String, String> {
        args.iter()
            .find_map(|arg| arg.strip_prefix(prefix))
            .map(str::to_string)
            .ok_or_else(|| format!("the update helper needs {prefix}<value>"))
    };
    let parent_pid = value(PARENT_PID_ARG)?
        .parse::<u32>()
        .map_err(|err| format!("{PARENT_PID_ARG} is not a process id: {err}"))?;
    let re_register_login_item = match value(RE_REGISTER_ARG)?.as_str() {
        "true" => true,
        "false" => false,
        other => return Err(format!("{RE_REGISTER_ARG}{other} is not true or false")),
    };
    Ok(ApplyRequest {
        parent_pid,
        target: PathBuf::from(value(TARGET_ARG)?),
        staged: PathBuf::from(value(STAGED_ARG)?),
        re_register_login_item,
        cleanup_dir: PathBuf::from(value(CLEANUP_ARG)?),
    })
}

/// apply swaps the bundle once the app exited and starts the updated app.
fn apply(request: &ApplyRequest) -> i32 {
    let log = request.cleanup_dir.join("apply-update.log");
    log_step(
        &log,
        "INFO",
        &format!("installing the update into {}", request.target.display()),
    );
    if let Err(err) = wait_for_exit(request.parent_pid) {
        // The bundles are swapped even then: a running app holds no file of
        // the bundle open for writing.
        log_step(&log, "WARN", &err);
    }
    if let Err(err) = swap(&request.target, &request.staged) {
        log_step(&log, "ERROR", &err);
        relaunch(&log, &request.target);
        return 1;
    }
    register(&log, &request.target);
    if request.re_register_login_item
        && let Err(err) = register_login_item(&request.target)
    {
        log_step(&log, "WARN", &err);
    }
    relaunch(&log, &request.target);
    if let Err(err) = fs::remove_dir_all(&request.cleanup_dir) {
        log_step(
            &log,
            "WARN",
            &format!("cannot remove the staging directory: {err}"),
        );
    }
    0
}

/// wait_for_exit waits until the app of the given process id is gone.
fn wait_for_exit(pid: u32) -> Result<(), String> {
    let deadline = Instant::now() + PARENT_EXIT_TIMEOUT;
    while Instant::now() < deadline {
        if !alive(pid) {
            return Ok(());
        }
        std::thread::sleep(PARENT_POLL);
    }
    Err(format!(
        "the running app did not exit within {} seconds",
        PARENT_EXIT_TIMEOUT.as_secs()
    ))
}

/// alive reports whether a process exists.
fn alive(pid: u32) -> bool {
    // SAFETY: the signal 0 only checks the process.
    let code = unsafe { libc::kill(pid as libc::pid_t, 0) };
    code == 0 || std::io::Error::last_os_error().raw_os_error() == Some(libc::EPERM)
}

/// swap replaces the installed bundle with the staged one and keeps the old
/// bundle as a backup until the new one is in place.
fn swap(target: &Path, staged: &Path) -> Result<(), String> {
    let backup = sibling(target, "bak");
    let _ = fs::remove_dir_all(&backup);
    fs::rename(target, &backup).map_err(|err| {
        format!(
            "cannot move the installed app {} aside: {err}",
            target.display()
        )
    })?;
    if let Err(err) = fs::rename(staged, target) {
        return Err(match fs::rename(&backup, target) {
            Ok(()) => format!("cannot put the new app in place: {err}"),
            Err(restore) => format!(
                "cannot put the new app in place: {err}; the previous app waits at {}: {restore}",
                backup.display()
            ),
        });
    }
    let _ = fs::remove_dir_all(&backup);
    Ok(())
}

/// register refreshes the registration of the bundle in Launch Services, so
/// Finder, the share extension and the URL handlers know the new version.
fn register(log: &Path, bundle: &Path) {
    let result = Command::new(LSREGISTER)
        .arg("-f")
        .arg(bundle)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
    if let Err(err) = result {
        log_step(log, "WARN", &format!("cannot register the app: {err}"));
    }
}

/// register_login_item lets the updated app register its login item again.
fn register_login_item(bundle: &Path) -> Result<(), String> {
    let executable = bundle.join(main_executable(bundle)?);
    let output = Command::new(&executable)
        .arg("--login-item=register")
        .stdin(Stdio::null())
        .output()
        .map_err(|err| format!("cannot register the login item: {err}"))?;
    if output.status.success() {
        return Ok(());
    }
    Err(format!(
        "cannot register the login item: {}",
        details(&output)
    ))
}

/// relaunch opens the bundle again.
fn relaunch(log: &Path, bundle: &Path) {
    let result = Command::new(OPEN)
        .arg("-n")
        .arg(bundle)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
    if let Err(err) = result {
        log_step(log, "ERROR", &format!("cannot start the app again: {err}"));
    }
}

/// log_step appends a line to the log of the helper and to the application log.
fn log_step(log: &Path, level: &str, message: &str) {
    crate::journal::record(level, message);
    let Ok(mut file) = fs::OpenOptions::new().create(true).append(true).open(log) else {
        return;
    };
    let _ = file.set_permissions(fs::Permissions::from_mode(0o600));
    let _ = writeln!(file, "{level} {message}");
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::testutil::TempDir;

    /// args returns command line arguments of a test.
    fn args(list: &[&str]) -> Vec<String> {
        list.iter().map(|arg| arg.to_string()).collect()
    }

    #[test]
    fn the_arguments_of_the_regular_start_ask_for_no_helper() {
        assert_eq!(parse_apply_args(&args(&[])), None);
        assert_eq!(parse_apply_args(&args(&["--login-item=status"])), None);
    }

    #[test]
    fn the_helper_reads_its_request() {
        let request = parse_apply_args(&args(&[
            APPLY_ARG,
            "--parent-pid=42",
            "--target=/Applications/Unison.app",
            "--staged=/Applications/.Unison.app.new.42",
            "--re-register-login-item=true",
            "--cleanup-dir=/Users/someone/Library/Caches/app.unison/updates/apply-42",
        ]))
        .expect("a helper run")
        .expect("a valid request");
        assert_eq!(
            request,
            ApplyRequest {
                parent_pid: 42,
                target: PathBuf::from("/Applications/Unison.app"),
                staged: PathBuf::from("/Applications/.Unison.app.new.42"),
                re_register_login_item: true,
                cleanup_dir: PathBuf::from(
                    "/Users/someone/Library/Caches/app.unison/updates/apply-42"
                ),
            }
        );
    }

    #[test]
    fn request_round_trips_through_helper_args() {
        let original = ApplyRequest {
            parent_pid: 1234,
            target: PathBuf::from("/Applications/Unison.app"),
            staged: PathBuf::from("/Applications/.Unison.app.new.1234"),
            re_register_login_item: false,
            cleanup_dir: PathBuf::from("/tmp/unison-apply-1234"),
        };
        let parsed = parse_apply_args(&original.to_args())
            .expect("a helper run")
            .expect("a valid request");
        assert_eq!(parsed, original);
    }

    #[test]
    fn the_helper_refuses_a_request_with_a_missing_option() {
        let err = parse_apply_args(&args(&[APPLY_ARG, "--parent-pid=42"]))
            .expect("a helper run")
            .expect_err("an incomplete request");
        assert!(err.contains("needs --"), "{err}");
        assert!(err.contains(RE_REGISTER_ARG), "{err}");
    }

    #[test]
    fn the_helper_refuses_a_broken_option() {
        let base = |pid: &str, re_register: &str| {
            args(&[
                APPLY_ARG,
                pid,
                "--target=/Applications/Unison.app",
                "--staged=/Applications/.Unison.app.new.42",
                re_register,
                "--cleanup-dir=/tmp/apply",
            ])
        };
        assert!(
            parse_apply_args(&base("--parent-pid=later", "--re-register-login-item=true"))
                .expect("a helper run")
                .is_err()
        );
        assert!(
            parse_apply_args(&base("--parent-pid=42", "--re-register-login-item=yes"))
                .expect("a helper run")
                .is_err()
        );
    }

    #[test]
    fn the_swap_replaces_the_bundle() {
        let dir = TempDir::new();
        let target = dir.join("Unison.app");
        let staged = dir.join(".Unison.app.new");
        fs::create_dir_all(target.join("old")).expect("create the installed bundle");
        fs::write(target.join("old/version"), b"0.2.0").expect("write the old bundle");
        fs::create_dir_all(&staged).expect("create the staged bundle");
        fs::write(staged.join("version"), b"0.2.1").expect("write the new bundle");

        swap(&target, &staged).expect("swap the bundles");

        assert_eq!(
            fs::read(target.join("version")).expect("read the new bundle"),
            b"0.2.1"
        );
        assert!(!target.join("old").exists(), "the old content stayed");
        assert!(!staged.exists(), "the staged bundle stayed");
        let backup = sibling(&target, "bak");
        assert!(!backup.exists(), "the backup stayed");
    }

    #[test]
    fn a_failed_swap_restores_the_bundle() {
        let dir = TempDir::new();
        let target = dir.join("Unison.app");
        fs::create_dir_all(target.join("old")).expect("create the installed bundle");
        fs::write(target.join("old/version"), b"0.2.0").expect("write the old bundle");
        let staged = dir.join(".Unison.app.new");

        let err = swap(&target, &staged).expect_err("a missing staged bundle");
        assert!(err.contains("cannot put the new app in place"), "{err}");
        assert_eq!(
            fs::read(target.join("old/version")).expect("read the restored bundle"),
            b"0.2.0"
        );
        assert!(!sibling(&target, "bak").exists(), "the backup stayed");
    }

    #[test]
    fn a_process_of_the_test_is_alive() {
        assert!(alive(std::process::id()));
        let mut child = Command::new("/bin/sh")
            .args(["-c", "exit 0"])
            .spawn()
            .expect("start the child");
        let pid = child.id();
        child.wait().expect("wait for the child");
        assert!(!alive(pid), "a finished process is alive");
    }
}
