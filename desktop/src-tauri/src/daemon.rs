//! Discovery and startup of the daemon.

use std::ffi::OsString;
use std::path::{Path, PathBuf};
use std::process::{Command, ExitStatus, Stdio};
use std::sync::{Mutex, MutexGuard};
use std::time::{Duration, Instant};

use serde::Serialize;

use crate::proxy;

/// ENDPOINT_ENV overrides the endpoint of the local API, like the clients of
/// the daemon do.
const ENDPOINT_ENV: &str = "UNISON_LOCAL_API";
/// BINARY_ENV points at the daemon binary of a development build.
pub(crate) const BINARY_ENV: &str = "UNISON_DAEMON_BIN";

/// Limits of the check and the startup of the daemon.
const PROBE_TIMEOUT: Duration = Duration::from_millis(500);
const START_TIMEOUT: Duration = Duration::from_secs(10);
const START_INTERVAL: Duration = Duration::from_millis(200);
/// EXIT_GRACE is how long the app keeps probing after the daemon it started
/// exited, before it reports the exit.
const EXIT_GRACE: Duration = Duration::from_secs(1);
/// LOGIN_WAIT is how long the app waits for the login item to start the daemon
/// before it starts the daemon of the bundle itself.
const LOGIN_WAIT: Duration = Duration::from_secs(3);

/// BINARY_NAME is the file name of the daemon next to the app binary.
const BINARY_NAME: &str = if cfg!(windows) {
    "unisond.exe"
} else {
    "unisond"
};

/// STARTED is the daemon the app started itself, which steps aside when the
/// login item takes over.
static STARTED: Mutex<Option<u32>> = Mutex::new(None);

/// LAST_EXIT is the process id and the exit status of the last daemon the app
/// started itself that exited. The id keeps the exit of an older daemon, which
/// the app stopped, from ending the wait for a newer one.
static LAST_EXIT: Mutex<Option<(u32, ExitStatus)>> = Mutex::new(None);

/// STATUS is what the app knows about the daemon, which the window reads.
static STATUS: Mutex<Status> = Mutex::new(Status {
    state: State::Offline,
    reason: None,
});

/// State of the daemon as the app knows it.
#[derive(Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum State {
    /// The daemon answers on the endpoint.
    Running,
    /// The daemon does not answer and no start has failed.
    Offline,
    /// The last start of the daemon failed.
    Failed,
}

/// Status of the daemon and the reason of the last failed start.
#[derive(Clone, Serialize)]
pub struct Status {
    pub state: State,
    /// Reason of the last failed start, absent when it is not known.
    pub reason: Option<String>,
}

/// status returns the state of the daemon and the reason of the last failed
/// start.
pub fn status() -> Status {
    lock(&STATUS).clone()
}

/// set_failed records a reason the daemon is not available for.
pub fn set_failed(reason: &str) {
    set_status(State::Failed, Some(reason.to_string()));
}

/// set_status records the state of the daemon.
fn set_status(state: State, reason: Option<String>) {
    let mut status = lock(&STATUS);
    status.state = state;
    status.reason = reason;
}

/// lock takes a mutex and keeps its contents after a panic of another thread,
/// so a failure never hides the state of the daemon.
fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(|err| err.into_inner())
}

/// resolve_endpoint returns the endpoint of the local API.
pub fn resolve_endpoint() -> Result<String, String> {
    endpoint_from(std::env::var_os(ENDPOINT_ENV), platform_endpoint)
}

/// endpoint_from returns the endpoint the override names, or the endpoint of
/// the platform when the override is absent or empty.
fn endpoint_from(
    custom: Option<OsString>,
    platform: impl FnOnce() -> Result<String, String>,
) -> Result<String, String> {
    if let Some(path) = custom {
        let path = path.to_string_lossy().into_owned();
        if !path.is_empty() {
            return Ok(path);
        }
    }
    platform()
}

/// Startup is what the app consults to bring the daemon up: the login item,
/// the binary it may start itself and the limits of the waits.
struct Startup {
    login: fn() -> crate::agent::LoginItemStatus,
    binary: Box<dyn Fn() -> Option<PathBuf> + Send + Sync>,
    login_wait: Duration,
    start_timeout: Duration,
}

impl Startup {
    /// system is the startup of the app: the login item of the system and the
    /// binary of the bundle.
    fn system() -> Self {
        Self {
            login: crate::agent::status,
            binary: Box::new(binary),
            login_wait: LOGIN_WAIT,
            start_timeout: START_TIMEOUT,
        }
    }
}

/// ensure_running starts the daemon when it does not answer on the endpoint and
/// keeps the reason of a start that failed.
pub async fn ensure_running(endpoint: &str) -> Result<(), String> {
    ensure_running_with(endpoint, &Startup::system()).await
}

/// ensure_running_with is ensure_running with the given startup.
async fn ensure_running_with(endpoint: &str, startup: &Startup) -> Result<(), String> {
    // A new attempt drops the reason of the previous one.
    set_status(State::Offline, None);
    match start_if_needed(endpoint, startup).await {
        Ok(()) => {
            set_status(State::Running, None);
            Ok(())
        }
        Err(err) => {
            set_status(State::Failed, Some(err.clone()));
            crate::observability::capture_error("the daemon did not start");
            Err(err)
        }
    }
}

/// start_if_needed starts the daemon when it does not answer on the endpoint. A
/// login item that the system manages owns the daemon, so the app only waits
/// for it.
async fn start_if_needed(endpoint: &str, startup: &Startup) -> Result<(), String> {
    if probe(endpoint).await {
        return Ok(());
    }

    match (startup.login)() {
        crate::agent::LoginItemStatus::Enabled => {
            if wait_for(endpoint, startup.login_wait).await.is_ok() {
                return Ok(());
            }
            // A login item that the system cannot start must not leave the app
            // without a daemon.
            match (startup.binary)() {
                Some(binary) => {
                    let pid = start(&binary)?;
                    wait_started(endpoint, startup.start_timeout, pid).await
                }
                None => Err(format!(
                    "the login item does not start the daemon on {endpoint}"
                )),
            }
        }
        crate::agent::LoginItemStatus::RequiresApproval => {
            Err("the login item waits for approval in System Settings".to_string())
        }
        _ => match (startup.binary)() {
            Some(binary) => {
                let pid = start(&binary)?;
                wait_started(endpoint, startup.start_timeout, pid).await
            }
            None => Err(format!("the daemon is not running on {endpoint}")),
        },
    }
}

/// wait_for waits for the daemon to answer on the endpoint.
async fn wait_for(endpoint: &str, limit: Duration) -> Result<(), String> {
    let deadline = Instant::now() + limit;
    while Instant::now() < deadline {
        tokio::time::sleep(START_INTERVAL).await;
        if probe(endpoint).await {
            return Ok(());
        }
    }
    Err(format!("the daemon did not answer on {endpoint}"))
}

/// wait_started waits for the daemon the app started as pid and reports why it
/// did not come up.
async fn wait_started(endpoint: &str, limit: Duration, pid: u32) -> Result<(), String> {
    wait_child(endpoint, limit, || exit_of(pid)).await
}

/// exit_of returns the exit status of the started daemon pid once it exited.
fn exit_of(pid: u32) -> Option<ExitStatus> {
    match *lock(&LAST_EXIT) {
        Some((exited, status)) if exited == pid => Some(status),
        _ => None,
    }
}

/// wait_child waits for a started daemon to answer on the endpoint. A daemon
/// that exits ends the wait after EXIT_GRACE instead of the whole limit; the
/// grace covers a daemon that exits because another instance is starting up
/// on the same endpoint.
async fn wait_child(
    endpoint: &str,
    limit: Duration,
    exited: impl Fn() -> Option<ExitStatus>,
) -> Result<(), String> {
    let deadline = Instant::now() + limit;
    let mut exit_deadline: Option<Instant> = None;
    loop {
        if probe(endpoint).await {
            return Ok(());
        }
        let now = Instant::now();
        if let Some(status) = exited() {
            let until = *exit_deadline.get_or_insert(now + EXIT_GRACE);
            if now >= until || now >= deadline {
                return Err(format!("the daemon stopped during startup ({status})"));
            }
        } else if now >= deadline {
            return Err(format!("the daemon did not answer on {endpoint}"));
        }
        tokio::time::sleep(START_INTERVAL).await;
    }
}

/// wait_stopped waits until nothing answers on the endpoint any more, which is
/// how the app notices that the system stopped the login item.
pub async fn wait_stopped(endpoint: &str, limit: Duration) {
    let deadline = Instant::now() + limit;
    while Instant::now() < deadline {
        if !probe(endpoint).await {
            return;
        }
        tokio::time::sleep(START_INTERVAL).await;
    }
}

/// probe reports whether a process answers on the endpoint.
async fn probe(endpoint: &str) -> bool {
    matches!(
        tokio::time::timeout(PROBE_TIMEOUT, proxy::connect(endpoint)).await,
        Ok(Ok(_))
    )
}

/// binary returns the daemon to start. A packaged app runs the daemon from the
/// autostart entry of the system, from the installation directory when the app
/// itself lives elsewhere, and from next to the app binary otherwise.
/// Development builds start the daemon from `bin/`.
pub fn binary() -> Option<PathBuf> {
    binary_from(std::env::var_os(BINARY_ENV), || {
        if cfg!(debug_assertions) {
            return Some(
                PathBuf::from(env!("CARGO_MANIFEST_DIR"))
                    .join("../../bin")
                    .join(BINARY_NAME),
            );
        }
        #[cfg(windows)]
        {
            // An installation made by the app or by the installer keeps the
            // daemon in the program files, where the startup entry points too.
            if let Some(dir) = crate::install::installed_dir(std::env::var_os("ProgramFiles")) {
                let installed = dir.join(BINARY_NAME);
                if installed.is_file() {
                    return Some(installed);
                }
            }
        }
        let exe = std::env::current_exe().ok()?;
        Some(exe.parent()?.join(BINARY_NAME))
    })
}

/// binary_from returns the binary the override names, or the default binary
/// when it exists.
fn binary_from(
    custom: Option<OsString>,
    default: impl FnOnce() -> Option<PathBuf>,
) -> Option<PathBuf> {
    if let Some(path) = custom {
        return Some(PathBuf::from(path));
    }
    default().filter(|path| path.is_file())
}

/// stop_started stops the daemon the app started, so the login item can run its
/// own. It reports whether that daemon was running.
pub fn stop_started() -> bool {
    let pid = STARTED.lock().ok().and_then(|mut slot| slot.take());
    let Some(pid) = pid else {
        return false;
    };
    // SIGTERM closes the store and the endpoint of the daemon.
    let _ = Command::new("/bin/kill")
        .args(["-TERM", &pid.to_string()])
        .status();
    true
}

/// start runs the daemon next to the app and returns its process id. The
/// daemon outlives the app, so the child is waited on in the background and
/// never killed.
fn start(binary: &PathBuf) -> Result<u32, String> {
    let mut child = Command::new(binary)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .map_err(|err| format!("cannot start the daemon {}: {err}", binary.display()))?;
    let pid = child.id();
    if let Ok(mut slot) = STARTED.lock() {
        *slot = Some(pid);
    }
    std::thread::spawn(move || {
        if let Ok(status) = child.wait() {
            *lock(&LAST_EXIT) = Some((pid, status));
        }
        if let Ok(mut slot) = STARTED.lock()
            && *slot == Some(pid)
        {
            *slot = None;
        }
    });
    Ok(pid)
}

/// platform_endpoint returns the endpoint the daemon of this user serves.
fn platform_endpoint() -> Result<String, String> {
    #[cfg(unix)]
    {
        unix_endpoint(std::env::var_os("HOME"), std::env::var_os("XDG_DATA_HOME"))
    }
    #[cfg(windows)]
    {
        windows_endpoint()
    }
}

/// data_dir returns the directory of the database, the logs and the lock file,
/// the same way the daemon resolves it.
pub fn data_dir() -> Result<PathBuf, String> {
    #[cfg(unix)]
    {
        unix_data_dir(std::env::var_os("HOME"), std::env::var_os("XDG_DATA_HOME"))
    }
    #[cfg(windows)]
    {
        let root = std::env::var_os("LOCALAPPDATA")
            .filter(|root| !root.is_empty())
            .ok_or_else(|| "the local application data directory is not set".to_string())?;
        Ok(PathBuf::from(root).join("Unison"))
    }
}

/// unix_data_dir returns the data directory under the given home directory.
/// On Linux the XDG data home wins when it is an absolute path.
#[cfg(unix)]
#[cfg_attr(not(target_os = "linux"), allow(unused_variables))]
fn unix_data_dir(
    home: Option<OsString>,
    xdg_data_home: Option<OsString>,
) -> Result<PathBuf, String> {
    let home = home
        .filter(|home| !home.is_empty())
        .ok_or_else(|| "the home directory is not set".to_string())?;
    let mut dir = PathBuf::from(home);
    #[cfg(target_os = "macos")]
    {
        dir.push("Library/Application Support");
        dir.push("Unison");
    }
    #[cfg(target_os = "linux")]
    {
        dir.push(linux_data_root(xdg_data_home));
        dir.push("unison");
    }
    #[cfg(not(any(target_os = "macos", target_os = "linux")))]
    {
        dir.push(".local/share");
        dir.push("unison");
    }
    Ok(dir)
}

/// linux_data_root returns the data root of the daemon on Linux: the XDG data
/// home when it is an absolute path, the default data home otherwise.
#[cfg(unix)]
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
fn linux_data_root(xdg_data_home: Option<OsString>) -> PathBuf {
    match xdg_data_home {
        Some(xdg) if Path::new(&xdg).is_absolute() => PathBuf::from(xdg),
        _ => PathBuf::from(".local/share"),
    }
}

/// unix_endpoint returns the socket of the daemon in the data directory under
/// the given home directory.
#[cfg(unix)]
fn unix_endpoint(
    home: Option<OsString>,
    xdg_data_home: Option<OsString>,
) -> Result<String, String> {
    let mut dir = unix_data_dir(home, xdg_data_home)?;
    dir.push("unisond.sock");
    Ok(dir.to_string_lossy().into_owned())
}

/// windows_endpoint returns the named pipe of the current user.
#[cfg(windows)]
fn windows_endpoint() -> Result<String, String> {
    use windows_sys::Win32::Foundation::{CloseHandle, HANDLE, LocalFree};
    use windows_sys::Win32::Security::Authorization::ConvertSidToStringSidW;
    use windows_sys::Win32::Security::{GetTokenInformation, TOKEN_QUERY, TOKEN_USER, TokenUser};
    use windows_sys::Win32::System::Threading::{GetCurrentProcess, OpenProcessToken};

    // SAFETY: the token and the buffers are owned by the calls below, and the
    // strings are copied before the memory is released.
    unsafe {
        let mut token: HANDLE = std::ptr::null_mut();
        if OpenProcessToken(GetCurrentProcess(), TOKEN_QUERY, &mut token) == 0 {
            return Err("cannot read the user of this process".to_string());
        }

        let mut size = 0u32;
        GetTokenInformation(token, TokenUser, std::ptr::null_mut(), 0, &mut size);
        let mut buffer = vec![0u8; size as usize];
        let ok = GetTokenInformation(
            token,
            TokenUser,
            buffer.as_mut_ptr().cast(),
            size,
            &mut size,
        );
        CloseHandle(token);
        if ok == 0 || size == 0 {
            return Err("cannot read the user of this process".to_string());
        }

        let user = &*(buffer.as_ptr() as *const TOKEN_USER);
        let mut value: *mut u16 = std::ptr::null_mut();
        if ConvertSidToStringSidW(user.User.Sid, &mut value) == 0 {
            return Err("cannot read the user of this process".to_string());
        }
        let length = (0..).take_while(|i| *value.add(*i) != 0).count();
        let sid = String::from_utf16_lossy(std::slice::from_raw_parts(value, length));
        LocalFree(value.cast());
        Ok(format!(r"\\.\pipe\unison-{sid}"))
    }
}

#[cfg(all(test, unix))]
mod tests {
    use super::*;
    use crate::agent::LoginItemStatus;
    use crate::testutil::{TempDir, bind_socket, block_on, set_mode};
    use std::os::unix::process::ExitStatusExt;
    use std::path::Path;

    /// GLOBALS serializes the tests that change the state the app keeps in
    /// statics: the status, the started daemon and its exit.
    static GLOBALS: Mutex<()> = Mutex::new(());

    fn globals() -> MutexGuard<'static, ()> {
        let guard = lock(&GLOBALS);
        *lock(&STARTED) = None;
        *lock(&LAST_EXIT) = None;
        set_status(State::Offline, None);
        guard
    }

    /// startup returns a startup with short waits, the given login item and
    /// the given binary.
    fn startup(login: fn() -> LoginItemStatus, binary: Option<PathBuf>) -> Startup {
        Startup {
            login,
            binary: Box::new(move || binary.clone()),
            login_wait: Duration::from_millis(400),
            start_timeout: Duration::from_secs(5),
        }
    }

    /// script writes an executable shell script that stands in for the daemon.
    fn script(dir: &TempDir, name: &str, body: &str) -> PathBuf {
        let path = dir.join(name);
        std::fs::write(&path, format!("#!/bin/sh\n{body}\n")).expect("write the script");
        set_mode(&path, 0o755);
        path
    }

    /// serve_later binds the socket of the daemon after a delay, like a
    /// daemon that takes a moment to come up.
    fn serve_later(
        path: &Path,
        delay: Duration,
    ) -> tokio::task::JoinHandle<std::os::unix::net::UnixListener> {
        let path = path.to_path_buf();
        tokio::spawn(async move {
            tokio::time::sleep(delay).await;
            bind_socket(&path)
        })
    }

    /// wait_exit waits for the started daemon pid to exit.
    fn wait_exit(pid: u32) -> Option<ExitStatus> {
        let deadline = Instant::now() + Duration::from_secs(5);
        while Instant::now() < deadline {
            if let Some(status) = exit_of(pid) {
                return Some(status);
            }
            std::thread::sleep(Duration::from_millis(20));
        }
        None
    }

    fn enabled() -> LoginItemStatus {
        LoginItemStatus::Enabled
    }

    fn not_registered() -> LoginItemStatus {
        LoginItemStatus::NotRegistered
    }

    fn requires_approval() -> LoginItemStatus {
        LoginItemStatus::RequiresApproval
    }

    fn unreachable_login() -> LoginItemStatus {
        panic!("the login item was consulted although the daemon answers")
    }

    #[test]
    fn wait_child_returns_once_the_daemon_answers() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let _listener = bind_socket(&path);
        let started = Instant::now();
        let result = block_on(wait_child(path.to_str().unwrap(), START_TIMEOUT, || None));
        assert_eq!(result, Ok(()));
        assert!(started.elapsed() < Duration::from_secs(1));
    }

    #[test]
    fn wait_child_reports_an_early_exit_without_waiting_for_the_limit() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let started = Instant::now();
        let result = block_on(wait_child(path.to_str().unwrap(), START_TIMEOUT, || {
            Some(ExitStatus::from_raw(1 << 8))
        }));
        let err = result.expect_err("a daemon that exited was reported as running");
        assert!(err.contains("stopped during startup"), "{err}");
        assert!(
            started.elapsed() < EXIT_GRACE + Duration::from_secs(1),
            "the wait took {:?}",
            started.elapsed()
        );
    }

    #[test]
    fn wait_child_accepts_a_daemon_that_answers_during_the_grace() {
        // A daemon that exits because another instance takes the endpoint
        // leaves that instance serving, which is a successful start.
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let result = block_on(async {
            let _server = serve_later(&path, Duration::from_millis(300));
            wait_child(path.to_str().unwrap(), START_TIMEOUT, || {
                Some(ExitStatus::from_raw(1 << 8))
            })
            .await
        });
        assert_eq!(result, Ok(()));
    }

    #[test]
    fn wait_child_reports_a_daemon_that_never_answers() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let result = block_on(wait_child(
            path.to_str().unwrap(),
            Duration::from_millis(300),
            || None,
        ));
        let err = result.expect_err("a silent daemon was reported as running");
        assert!(err.contains("did not answer"), "{err}");
    }

    #[test]
    fn wait_stopped_returns_when_nothing_answers() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let started = Instant::now();
        block_on(wait_stopped(path.to_str().unwrap(), Duration::from_secs(5)));
        assert!(started.elapsed() < Duration::from_secs(1));
    }

    #[test]
    fn wait_stopped_returns_once_the_daemon_goes_away() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let listener = bind_socket(&path);
        let started = Instant::now();
        block_on(async {
            let socket = path.clone();
            tokio::spawn(async move {
                tokio::time::sleep(Duration::from_millis(300)).await;
                drop(listener);
                let _ = std::fs::remove_file(socket);
            });
            wait_stopped(path.to_str().unwrap(), Duration::from_secs(5)).await;
        });
        let elapsed = started.elapsed();
        assert!(
            elapsed >= Duration::from_millis(300) && elapsed < Duration::from_secs(2),
            "the wait took {elapsed:?}"
        );
    }

    #[test]
    fn wait_stopped_gives_up_after_the_limit() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let _listener = bind_socket(&path);
        let started = Instant::now();
        block_on(wait_stopped(
            path.to_str().unwrap(),
            Duration::from_millis(300),
        ));
        assert!(started.elapsed() < Duration::from_secs(2));
    }

    #[test]
    fn probe_refuses_an_endpoint_other_users_can_reach() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let _listener = bind_socket(&path);
        set_mode(&path, 0o666);
        assert!(!block_on(probe(path.to_str().unwrap())));
    }

    #[test]
    fn ensure_running_reports_a_daemon_that_already_answers() {
        let _globals = globals();
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let _listener = bind_socket(&path);
        set_failed("an earlier failure");
        let startup = Startup {
            login: unreachable_login,
            binary: Box::new(|| panic!("a binary was looked up although the daemon answers")),
            login_wait: Duration::ZERO,
            start_timeout: Duration::ZERO,
        };
        let result = block_on(ensure_running_with(path.to_str().unwrap(), &startup));
        assert_eq!(result, Ok(()));
        let status = status();
        assert!(status.state == State::Running);
        assert_eq!(
            status.reason, None,
            "the reason of an earlier failure stayed"
        );
        assert!(!stop_started(), "a running daemon was started again");
    }

    #[test]
    fn ensure_running_keeps_the_reason_of_a_failed_start() {
        let _globals = globals();
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let result = block_on(ensure_running_with(
            path.to_str().unwrap(),
            &startup(not_registered, None),
        ));
        let err = result.expect_err("a missing daemon was reported as running");
        assert!(err.contains("not running"), "{err}");
        let status = status();
        assert!(status.state == State::Failed);
        assert_eq!(status.reason, Some(err));
    }

    #[test]
    fn a_login_item_that_waits_for_approval_is_reported() {
        let _globals = globals();
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let binary = script(&dir, "unisond", "exec sleep 30");
        let err = block_on(start_if_needed(
            path.to_str().unwrap(),
            &startup(requires_approval, Some(binary)),
        ))
        .expect_err("a login item without approval started the daemon");
        assert!(err.contains("approval"), "{err}");
        assert!(!stop_started(), "the app started its own daemon");
    }

    #[test]
    fn the_login_item_starts_the_daemon() {
        let _globals = globals();
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let result = block_on(async {
            let server = serve_later(&path, Duration::from_millis(100));
            let result = start_if_needed(path.to_str().unwrap(), &startup(enabled, None)).await;
            (result, server.await)
        });
        assert_eq!(result.0, Ok(()));
        assert!(!stop_started(), "the app started its own daemon");
    }

    #[test]
    fn a_login_item_that_does_not_start_the_daemon_without_a_binary_is_reported() {
        let _globals = globals();
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let err = block_on(start_if_needed(
            path.to_str().unwrap(),
            &startup(enabled, None),
        ))
        .expect_err("a silent login item was reported as running");
        assert!(err.contains("login item does not start"), "{err}");
    }

    #[test]
    fn the_app_starts_its_daemon_when_the_login_item_does_not() {
        let _globals = globals();
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let binary = script(&dir, "unisond", "exec sleep 30");
        let started = Instant::now();
        let result = block_on(async {
            // The socket appears only after the wait for the login item.
            let server = serve_later(&path, Duration::from_millis(800));
            let result =
                start_if_needed(path.to_str().unwrap(), &startup(enabled, Some(binary))).await;
            (result, server.await)
        });
        assert_eq!(result.0, Ok(()));
        assert!(started.elapsed() >= Duration::from_millis(400));

        let pid = lock(&STARTED).expect("the started daemon is not kept");
        assert!(stop_started(), "the started daemon was not stopped");
        assert!(!stop_started(), "the daemon was stopped twice");
        let status = wait_exit(pid).expect("the stopped daemon kept running");
        assert_eq!(status.signal(), Some(libc::SIGTERM));
    }

    #[test]
    fn the_app_starts_its_daemon_without_a_login_item() {
        let _globals = globals();
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let binary = script(&dir, "unisond", "exec sleep 30");
        let started = Instant::now();
        let result = block_on(async {
            let server = serve_later(&path, Duration::from_millis(100));
            let result = start_if_needed(
                path.to_str().unwrap(),
                &startup(not_registered, Some(binary)),
            )
            .await;
            (result, server.await)
        });
        assert_eq!(result.0, Ok(()));
        assert!(
            started.elapsed() < Duration::from_millis(400),
            "the app waited for a login item that is not registered"
        );
        assert!(stop_started(), "the started daemon was not kept");
    }

    #[test]
    fn a_daemon_that_exits_during_startup_is_reported_with_its_status() {
        let _globals = globals();
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let binary = script(&dir, "unisond", "exit 3");
        let started = Instant::now();
        let err = block_on(start_if_needed(
            path.to_str().unwrap(),
            &startup(not_registered, Some(binary)),
        ))
        .expect_err("a daemon that exited was reported as running");
        assert!(err.contains("stopped during startup"), "{err}");
        assert!(err.contains('3'), "the exit status is missing: {err}");
        assert!(started.elapsed() < Duration::from_secs(3));
        assert!(!stop_started(), "an exited daemon is still kept");
    }

    #[test]
    fn a_binary_that_cannot_run_is_reported() {
        let _globals = globals();
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let binary = dir.join("missing");
        let err = block_on(start_if_needed(
            path.to_str().unwrap(),
            &startup(not_registered, Some(binary)),
        ))
        .expect_err("a missing binary was started");
        assert!(err.starts_with("cannot start the daemon"), "{err}");
    }

    #[test]
    fn the_exit_of_an_older_daemon_does_not_end_the_wait_for_a_newer_one() {
        let _globals = globals();
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let binary = script(&dir, "unisond", "exec sleep 30");
        let older = start(&binary).expect("start the older daemon");
        let newer = start(&binary).expect("start the newer daemon");
        // The older daemon exits only after the newer one started, as when the
        // app stops its daemon for the login item and starts it again.
        // SAFETY: kill only sends a signal to the child the test started.
        unsafe { libc::kill(older as libc::pid_t, libc::SIGTERM) };
        wait_exit(older).expect("the older daemon kept running");
        assert_eq!(exit_of(newer), None, "the newer daemon took the older exit");
        let result = block_on(async {
            let server = serve_later(&path, Duration::from_millis(1500));
            let result = wait_started(path.to_str().unwrap(), START_TIMEOUT, newer).await;
            (result, server.await)
        });
        assert_eq!(result.0, Ok(()));
        assert!(stop_started());
    }

    #[test]
    fn stop_started_without_a_started_daemon_does_nothing() {
        let _globals = globals();
        assert!(!stop_started());
    }

    #[test]
    fn status_serializes_for_the_window() {
        let status = Status {
            state: State::Failed,
            reason: Some("boom".to_string()),
        };
        assert_eq!(
            serde_json::to_string(&status).unwrap(),
            r#"{"state":"failed","reason":"boom"}"#
        );
        let status = Status {
            state: State::Running,
            reason: None,
        };
        assert_eq!(
            serde_json::to_string(&status).unwrap(),
            r#"{"state":"running","reason":null}"#
        );
        let status = Status {
            state: State::Offline,
            reason: None,
        };
        assert!(
            serde_json::to_string(&status)
                .unwrap()
                .contains(r#""offline""#)
        );
    }

    #[test]
    fn endpoint_from_prefers_the_override() {
        let platform = || -> Result<String, String> { panic!("the platform was consulted") };
        assert_eq!(
            endpoint_from(Some("/tmp/custom.sock".into()), platform),
            Ok("/tmp/custom.sock".to_string())
        );
    }

    #[test]
    fn endpoint_from_ignores_an_empty_override() {
        assert_eq!(
            endpoint_from(Some(OsString::new()), || Ok("platform".to_string())),
            Ok("platform".to_string())
        );
        assert_eq!(
            endpoint_from(None, || Err("no home".to_string())),
            Err("no home".to_string())
        );
    }

    #[test]
    fn unix_endpoint_lives_in_the_data_directory() {
        let endpoint = unix_endpoint(Some("/Users/someone".into()), None).expect("endpoint");
        #[cfg(target_os = "macos")]
        assert_eq!(
            endpoint,
            "/Users/someone/Library/Application Support/Unison/unisond.sock"
        );
        #[cfg(not(target_os = "macos"))]
        assert_eq!(endpoint, "/Users/someone/.local/share/unison/unisond.sock");
    }

    #[test]
    fn unix_endpoint_needs_a_home_directory() {
        assert!(unix_endpoint(None, None).is_err());
        assert!(
            unix_endpoint(Some(OsString::new()), None).is_err(),
            "an empty home turned into a relative endpoint"
        );
    }

    #[test]
    fn linux_data_root_follows_the_xdg_data_home() {
        assert_eq!(
            linux_data_root(Some("/data".into())),
            PathBuf::from("/data")
        );
    }

    #[test]
    fn linux_data_root_ignores_a_relative_xdg_data_home() {
        for xdg in [None, Some(OsString::new()), Some("data".into())] {
            assert_eq!(
                linux_data_root(xdg),
                PathBuf::from(".local/share"),
                "a relative or empty data home moved the data root"
            );
        }
    }

    #[cfg(target_os = "macos")]
    #[test]
    fn unix_data_dir_ignores_the_xdg_data_home() {
        let dir =
            unix_data_dir(Some("/Users/someone".into()), Some("/data".into())).expect("data dir");
        assert_eq!(
            dir,
            PathBuf::from("/Users/someone/Library/Application Support/Unison")
        );
    }

    #[test]
    fn binary_from_prefers_the_override() {
        let binary = binary_from(Some("/opt/unisond".into()), || {
            panic!("the default binary was consulted")
        });
        assert_eq!(binary, Some(PathBuf::from("/opt/unisond")));
    }

    #[test]
    fn binary_from_needs_an_existing_default() {
        let dir = TempDir::new();
        let path = dir.join("unisond");
        assert_eq!(binary_from(None, || Some(path.clone())), None);
        std::fs::write(&path, b"").unwrap();
        assert_eq!(binary_from(None, || Some(path.clone())), Some(path.clone()));
        assert_eq!(binary_from(None, || Some(dir.path().to_path_buf())), None);
        assert_eq!(binary_from(None, || None), None);
    }
}
