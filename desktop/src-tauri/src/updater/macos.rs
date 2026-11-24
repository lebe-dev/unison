//! Installation of an update on macOS.
//!
//! The verified disk image is mounted, the bundle inside it is checked and
//! copied next to the installed app. A running bundle cannot replace itself, so
//! the swap happens in a detached copy of this binary: it waits for the app to
//! exit, renames the bundles, refreshes the registration of Launch Services,
//! registers the login item again and opens the updated app.

use std::ffi::OsStr;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

use tauri::AppHandle;

use super::InstallPhase;

/// The helpers of the system that mount, copy and sign a bundle.
const HDIUTIL: &str = "/usr/bin/hdiutil";
const DITTO: &str = "/usr/bin/ditto";
const XATTR: &str = "/usr/bin/xattr";
const CODESIGN: &str = "/usr/bin/codesign";
const PLIST_BUDDY: &str = "/usr/libexec/PlistBuddy";
const OPEN: &str = "/usr/bin/open";
const PS: &str = "/bin/ps";
const LSREGISTER: &str = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister";

/// The argument that turns the binary into the helper of an installation.
const APPLY_ARG: &str = "--apply-update";
const PARENT_PID_ARG: &str = "--parent-pid=";
const TARGET_ARG: &str = "--target=";
const STAGED_ARG: &str = "--staged=";
const RE_REGISTER_ARG: &str = "--re-register-login-item=";
const CLEANUP_ARG: &str = "--cleanup-dir=";

/// The names of the release artifact and of the bundle inside it.
const BUNDLE_NAME: &str = "Unison.app";
const BUNDLE_IDENTIFIER: &str = "app.unison";
const INFO_PLIST: &str = "Contents/Info.plist";
const DAEMON_EXECUTABLE: &str = "Contents/MacOS/unisond";
const LOGIN_ITEM: &str = "Contents/Library/LaunchAgents/app.unison.unisond.plist";

/// How long the installer waits for the app and the daemon to exit.
const PARENT_EXIT_TIMEOUT: Duration = Duration::from_secs(10);
const PARENT_POLL: Duration = Duration::from_millis(100);
const DAEMON_STOP_TIMEOUT: Duration = Duration::from_secs(5);
const DAEMON_KILL_TIMEOUT: Duration = Duration::from_secs(3);
const PROBE_TIMEOUT: Duration = Duration::from_millis(500);

/// The pause before the app ends, so the window keeps its last status.
const QUIT_GRACE: Duration = Duration::from_millis(300);

/// asset_match reports whether a file of a release is the artifact of macOS.
pub fn asset_match(filename: &str, version: &str) -> bool {
    filename == format!("unison-v{version}-macos.dmg")
}

/// can_self_update reports why the app cannot replace itself here.
pub fn can_self_update() -> Result<(), String> {
    if cfg!(debug_assertions) {
        return Err("in-app updates are disabled in development builds".to_string());
    }
    let executable =
        std::env::current_exe().map_err(|err| format!("cannot find the running app: {err}"))?;
    let bundle = bundle_of(&executable)?;
    inspect(&bundle)
}

/// bundle_of returns the bundle that holds an executable of
/// `Contents/MacOS`.
fn bundle_of(executable: &Path) -> Result<PathBuf, String> {
    let bundle = executable
        .parent()
        .and_then(Path::parent)
        .and_then(Path::parent)
        .map(Path::to_path_buf)
        .ok_or_else(|| not_a_bundle(executable))?;
    if bundle.extension() != Some(OsStr::new("app")) {
        return Err(not_a_bundle(executable));
    }
    Ok(bundle)
}

/// not_a_bundle describes an executable that does not live in a bundle.
fn not_a_bundle(executable: &Path) -> String {
    format!(
        "{} is not inside an application bundle",
        executable.display()
    )
}

/// inspect reports why the bundle cannot be replaced from here.
fn inspect(bundle: &Path) -> Result<(), String> {
    if !bundle.join(INFO_PLIST).is_file() {
        return Err("the application bundle is incomplete".to_string());
    }
    if bundle.to_string_lossy().contains("/AppTranslocation/") {
        return Err(
            "Unison runs from a disk image or a temporary location. Move Unison.app to /Applications and start it there."
                .to_string(),
        );
    }
    if is_read_only(bundle) {
        return Err(
            "Unison runs from a read-only volume. Move Unison.app to /Applications and start it there."
                .to_string(),
        );
    }
    let parent = bundle
        .parent()
        .ok_or_else(|| "the installation folder of Unison is unknown".to_string())?;
    if !is_writable(parent) {
        return Err(
            "Unison does not have write permission to the installation folder. Move Unison.app to ~/Applications or update as an administrator."
                .to_string(),
        );
    }
    Ok(())
}

/// is_read_only reports whether the file system of a path is mounted read-only.
fn is_read_only(path: &Path) -> bool {
    use std::os::unix::ffi::OsStrExt;

    let Ok(path) = std::ffi::CString::new(path.as_os_str().as_bytes()) else {
        return false;
    };
    // SAFETY: the call fills the structure with the data of the file system.
    let mut stats: libc::statvfs = unsafe { std::mem::zeroed() };
    // SAFETY: the path is a null terminated string and the buffer is large
    // enough for the structure.
    let code = unsafe { libc::statvfs(path.as_ptr(), &mut stats) };
    code == 0 && stats.f_flag & libc::ST_RDONLY as libc::c_ulong != 0
}

/// is_writable reports whether the current user may write into a directory.
fn is_writable(dir: &Path) -> bool {
    use std::os::unix::ffi::OsStrExt;

    let Ok(dir) = std::ffi::CString::new(dir.as_os_str().as_bytes()) else {
        return false;
    };
    // SAFETY: the path is a null terminated string.
    unsafe { libc::access(dir.as_ptr(), libc::W_OK) == 0 }
}

/// run_arg handles the flags the installer starts the app with.
pub fn run_arg() -> Option<i32> {
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

/// ApplyRequest is everything the detached helper needs for the swap.
#[derive(Clone, Debug, PartialEq, Eq)]
struct ApplyRequest {
    parent_pid: u32,
    target: PathBuf,
    staged: PathBuf,
    re_register_login_item: bool,
    cleanup_dir: PathBuf,
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

/// sibling returns a hidden neighbour of a bundle, such as the staged copy or
/// the backup of the swap.
fn sibling(bundle: &Path, tag: &str) -> PathBuf {
    let name = bundle
        .file_name()
        .map(|name| name.to_string_lossy().into_owned())
        .unwrap_or_else(|| BUNDLE_NAME.to_string());
    bundle.with_file_name(format!(".{name}.{tag}.{}", std::process::id()))
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

/// main_executable returns the executable of a bundle that the Info.plist
/// names.
fn main_executable(bundle: &Path) -> Result<String, String> {
    let name = plist_value(bundle, "CFBundleExecutable")?;
    let name = name.trim();
    if name.is_empty() || name.contains('/') {
        return Err("the app names no executable".to_string());
    }
    Ok(format!("Contents/MacOS/{name}"))
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
    use std::io::Write;

    crate::journal::record(level, message);
    let Ok(mut file) = fs::OpenOptions::new().create(true).append(true).open(log) else {
        return;
    };
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = file.set_permissions(fs::Permissions::from_mode(0o600));
    }
    let _ = writeln!(file, "{level} {message}");
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
    on_phase(InstallPhase::Preparing);

    let target = bundle_of(
        &std::env::current_exe().map_err(|err| format!("cannot find the running app: {err}"))?,
    )?;
    let root = super::verify::staging_root(app).map_err(|err| err.message)?;
    let nonce = nonce();

    let staged = {
        let mount = Mount::attach(
            artifact_path,
            &root.join(format!("mnt-{}-{nonce}", std::process::id())),
        )?;
        let bundle = mount.dir.join(BUNDLE_NAME);
        check_bundle(&bundle, expected_version)?;
        stage(&bundle, &target)?
    };

    unquarantine(&staged);
    if let Err(err) = verify_signature(&staged) {
        let _ = fs::remove_dir_all(&staged);
        return Err(err);
    }

    on_phase(InstallPhase::StoppingDaemon);
    let login_item = crate::agent::status() == crate::agent::LoginItemStatus::Enabled;
    if let Err(err) = stop_daemon(endpoint, &target, login_item).await {
        if login_item {
            let _ = crate::agent::set(true);
        }
        let _ = fs::remove_dir_all(&staged);
        return Err(err);
    }

    on_phase(InstallPhase::ApplyingUpdate);
    let request = ApplyRequest {
        parent_pid: std::process::id(),
        target: target.clone(),
        staged,
        re_register_login_item: login_item,
        cleanup_dir: root.join(format!("apply-{}-{nonce}", std::process::id())),
    };
    if let Err(err) = spawn_helper(&request) {
        if login_item {
            let _ = crate::agent::set(true);
        }
        if let Some(endpoint) = endpoint {
            let _ = crate::daemon::ensure_running(endpoint).await;
        }
        let _ = fs::remove_dir_all(&request.staged);
        return Err(err);
    }

    on_phase(InstallPhase::Relaunching);
    quit(app);
    Ok(())
}

/// Mount is the disk image of an update. It is detached and its mount point
/// removed when it leaves the scope.
struct Mount {
    dir: PathBuf,
}

impl Mount {
    /// attach mounts the disk image read-only at a private mount point.
    fn attach(dmg: &Path, dir: &Path) -> Result<Self, String> {
        create_private(dir)?;
        let output = Command::new(HDIUTIL)
            .args([
                "-nobrowse",
                "-readonly",
                "-noverify",
                "-noautoopen",
                "-mountpoint",
            ])
            .arg(dir)
            .arg(dmg)
            .stdin(Stdio::null())
            .output()
            .map_err(|err| format!("cannot mount the update: {err}"))?;
        if output.status.success() {
            return Ok(Self {
                dir: dir.to_path_buf(),
            });
        }
        let _ = fs::remove_dir_all(dir);
        Err(format!("cannot mount the update: {}", details(&output)))
    }
}

impl Drop for Mount {
    fn drop(&mut self) {
        let _ = Command::new(HDIUTIL)
            .args(["-force", "detach"])
            .arg(&self.dir)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status();
        let _ = fs::remove_dir_all(&self.dir);
    }
}

/// check_bundle reports why the bundle of the disk image cannot replace the
/// installed app.
fn check_bundle(bundle: &Path, expected_version: &str) -> Result<(), String> {
    let metadata = fs::symlink_metadata(bundle)
        .map_err(|err| format!("the update holds no {BUNDLE_NAME}: {err}"))?;
    if metadata.file_type().is_symlink() || !metadata.is_dir() {
        return Err(format!("the update holds no {BUNDLE_NAME}"));
    }
    for relative in [INFO_PLIST, DAEMON_EXECUTABLE, LOGIN_ITEM] {
        if !bundle.join(relative).is_file() {
            return Err(format!("the update has no {relative}"));
        }
    }
    let identifier = plist_value(bundle, "CFBundleIdentifier")?;
    if identifier.trim() != BUNDLE_IDENTIFIER {
        return Err(format!(
            "the update is the app {}, not {BUNDLE_IDENTIFIER}",
            identifier.trim()
        ));
    }
    let version = plist_value(bundle, "CFBundleShortVersionString")?;
    if version.trim() != expected_version {
        return Err(format!(
            "the update is version {}, not {expected_version}",
            version.trim()
        ));
    }
    let executable = main_executable(bundle)?;
    if !bundle.join(&executable).is_file() {
        return Err(format!("the update has no {executable}"));
    }
    Ok(())
}

/// plist_value reads a key of the Info.plist of a bundle.
fn plist_value(bundle: &Path, key: &str) -> Result<String, String> {
    let plist = bundle.join(INFO_PLIST);
    let output = Command::new(PLIST_BUDDY)
        .arg("-c")
        .arg(format!("Print :{key}"))
        .arg(&plist)
        .stdin(Stdio::null())
        .output()
        .map_err(|err| format!("cannot read {INFO_PLIST}: {err}"))?;
    if !output.status.success() {
        return Err(format!("{INFO_PLIST} has no {key}"));
    }
    Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
}

/// stage copies the bundle of the disk image next to the installed app, so the
/// swap is a rename within one file system.
fn stage(source: &Path, target: &Path) -> Result<PathBuf, String> {
    let staged = sibling(target, "new");
    let _ = fs::remove_dir_all(&staged);
    let output = Command::new(DITTO)
        .arg(source)
        .arg(&staged)
        .stdin(Stdio::null())
        .output()
        .map_err(|err| format!("cannot copy the update: {err}"))?;
    if !output.status.success() {
        let _ = fs::remove_dir_all(&staged);
        return Err(format!("cannot copy the update: {}", details(&output)));
    }
    Ok(staged)
}

/// unquarantine removes the quarantine attribute, so an update that is not
/// notarized still starts after the bundle was copied.
fn unquarantine(bundle: &Path) {
    let _ = Command::new(XATTR)
        .args(["-dr", "com.apple.quarantine"])
        .arg(bundle)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
}

/// verify_signature checks the copied bundle and warns about an ad-hoc
/// signature, which runs but cannot be distributed.
fn verify_signature(bundle: &Path) -> Result<(), String> {
    let output = Command::new(CODESIGN)
        .args(["--verify", "--deep", "--strict"])
        .arg(bundle)
        .stdin(Stdio::null())
        .output()
        .map_err(|err| format!("cannot check the signature of the update: {err}"))?;
    if !output.status.success() {
        return Err(format!(
            "the signature of the update is broken: {}",
            details(&output)
        ));
    }
    let details = Command::new(CODESIGN)
        .args(["-dv", "--verbose=2"])
        .arg(bundle)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .output();
    if let Ok(details) = details
        && String::from_utf8_lossy(&details.stderr).contains("Signature=adhoc")
    {
        crate::journal::record(
            "WARN",
            "the update is signed ad-hoc, so the Keychain may ask for access again",
        );
    }
    Ok(())
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

/// spawn_helper starts the detached copy of this binary that swaps the bundle.
fn spawn_helper(request: &ApplyRequest) -> Result<(), String> {
    create_private(&request.cleanup_dir)?;
    let helper = request.cleanup_dir.join("unison-updater");
    let executable =
        std::env::current_exe().map_err(|err| format!("cannot find the running app: {err}"))?;
    fs::copy(&executable, &helper)
        .map_err(|err| format!("cannot copy the update helper: {err}"))?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(&helper, fs::Permissions::from_mode(0o700))
            .map_err(|err| format!("cannot make the update helper executable: {err}"))?;
    }
    let mut command = Command::new(&helper);
    command
        .arg(APPLY_ARG)
        .arg(format!("{PARENT_PID_ARG}{}", request.parent_pid))
        .arg(format!("{TARGET_ARG}{}", request.target.display()))
        .arg(format!("{STAGED_ARG}{}", request.staged.display()))
        .arg(format!(
            "{RE_REGISTER_ARG}{}",
            request.re_register_login_item
        ))
        .arg(format!("{CLEANUP_ARG}{}", request.cleanup_dir.display()))
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    // SAFETY: setsid only changes the session of the child before it runs, so
    // the helper survives the exit of the app.
    unsafe {
        use std::os::unix::process::CommandExt;
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

/// quit ends the app after the window showed the last phase.
fn quit(app: &AppHandle) {
    let app = app.clone();
    std::thread::spawn(move || {
        std::thread::sleep(QUIT_GRACE);
        app.exit(0);
    });
}

/// create_private creates a directory only the user can reach.
fn create_private(dir: &Path) -> Result<(), String> {
    fs::create_dir_all(dir).map_err(|err| format!("cannot create the staging directory: {err}"))?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(dir, fs::Permissions::from_mode(0o700))
            .map_err(|err| format!("cannot restrict the staging directory: {err}"))?;
    }
    Ok(())
}

/// nonce returns the number that keeps the directories of one run apart.
fn nonce() -> u128 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|elapsed| elapsed.as_nanos())
        .unwrap_or_default()
}

/// details returns the error output of a failed command.
fn details(output: &std::process::Output) -> String {
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
    use crate::testutil::TempDir;

    /// args returns command line arguments of a test.
    fn args(list: &[&str]) -> Vec<String> {
        list.iter().map(|arg| arg.to_string()).collect()
    }

    /// bundle writes a fake app bundle with the given version and identifier.
    fn bundle(dir: &TempDir, version: &str, identifier: &str) -> PathBuf {
        let app = dir.join(BUNDLE_NAME);
        for relative in [
            "Contents/MacOS/unison-desktop",
            DAEMON_EXECUTABLE,
            LOGIN_ITEM,
        ] {
            let path = app.join(relative);
            fs::create_dir_all(path.parent().expect("a parent")).expect("create the directories");
            fs::write(&path, b"").expect("write the file");
        }
        let plist = format!(
            r#"<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key>
  <string>{identifier}</string>
  <key>CFBundleShortVersionString</key>
  <string>{version}</string>
  <key>CFBundleExecutable</key>
  <string>unison-desktop</string>
</dict>
</plist>
"#
        );
        fs::create_dir_all(app.join("Contents")).expect("create the contents");
        fs::write(app.join(INFO_PLIST), plist).expect("write the plist");
        app
    }

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
    fn a_bundle_is_found_three_levels_above_the_executable() {
        let executable = Path::new("/Applications/Unison.app/Contents/MacOS/Unison");
        assert_eq!(
            bundle_of(executable),
            Ok(PathBuf::from("/Applications/Unison.app"))
        );
        assert!(bundle_of(Path::new("/usr/local/bin/unison")).is_err());
        assert!(bundle_of(Path::new("/Applications/Unison/Contents/MacOS/Unison")).is_err());
    }

    #[test]
    fn a_translocated_bundle_explains_the_move() {
        let dir = TempDir::new();
        let app = dir.join("AppTranslocation/d/Unison.app");
        fs::create_dir_all(app.join("Contents")).expect("create the contents");
        fs::write(app.join(INFO_PLIST), b"plist").expect("write the plist");
        let err = inspect(&app).expect_err("a translocated bundle");
        assert!(err.contains("Move Unison.app to /Applications"), "{err}");
    }

    #[test]
    fn a_bundle_of_the_user_needs_nothing_more() {
        let dir = TempDir::new();
        let app = dir.join("Unison.app");
        fs::create_dir_all(app.join("Contents")).expect("create the contents");
        fs::write(app.join(INFO_PLIST), b"plist").expect("write the plist");
        assert_eq!(inspect(&app), Ok(()));
    }

    #[test]
    fn an_incomplete_bundle_is_refused() {
        let dir = TempDir::new();
        let app = dir.join("Unison.app");
        assert!(inspect(&app).is_err());
    }

    #[test]
    fn the_folder_of_the_user_is_writable() {
        let dir = TempDir::new();
        assert!(is_writable(dir.path()));
        assert!(!is_writable(&dir.join("missing")));
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
    fn only_the_daemon_of_the_target_bundle_is_terminated() {
        let daemon = Path::new("/Applications/Unison.app/Contents/MacOS/unisond");
        let list = "  10 /Applications/Unison.app/Contents/MacOS/unisond\n  11 /Users/someone/pro/unison/bin/unisond\n  12 /usr/bin/unisond\n  13 /Applications/Unison.app/Contents/MacOS/unisond\n";
        assert_eq!(daemon_pids(list, daemon, daemon), [10, 13]);
        assert!(daemon_pids("  not a line\n", daemon, daemon).is_empty());
    }

    #[test]
    fn a_staged_bundle_must_carry_the_expected_app() {
        let dir = TempDir::new();
        let app = bundle(&dir, "0.2.1", BUNDLE_IDENTIFIER);
        assert_eq!(check_bundle(&app, "0.2.1"), Ok(()));

        let err = check_bundle(&app, "0.2.0").expect_err("another version");
        assert!(err.contains("0.2.1"), "{err}");

        let other = TempDir::new();
        let app = bundle(&other, "0.2.1", "app.other");
        let err = check_bundle(&app, "0.2.1").expect_err("another app");
        assert!(err.contains("app.other"), "{err}");
    }

    #[test]
    fn a_staged_bundle_needs_every_part_of_the_app() {
        let dir = TempDir::new();
        let app = bundle(&dir, "0.2.1", BUNDLE_IDENTIFIER);
        fs::remove_file(app.join(DAEMON_EXECUTABLE)).expect("remove the daemon");
        let err = check_bundle(&app, "0.2.1").expect_err("an app without a daemon");
        assert!(err.contains(DAEMON_EXECUTABLE), "{err}");

        let other = TempDir::new();
        let app = bundle(&other, "0.2.1", BUNDLE_IDENTIFIER);
        fs::remove_file(app.join("Contents/MacOS/unison-desktop")).expect("remove the app");
        let err = check_bundle(&app, "0.2.1").expect_err("an app without its executable");
        assert!(err.contains("unison-desktop"), "{err}");
    }

    #[test]
    fn a_missing_bundle_is_refused() {
        let dir = TempDir::new();
        let err = check_bundle(&dir.join(BUNDLE_NAME), "0.2.1").expect_err("a missing bundle");
        assert!(err.contains(BUNDLE_NAME), "{err}");
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

    #[test]
    fn a_development_build_does_not_update_itself() {
        assert_eq!(
            can_self_update().expect_err("a development build replaced itself"),
            "in-app updates are disabled in development builds"
        );
    }
}
