//! Discovery and startup of the daemon.

use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::sync::Mutex;
use std::time::{Duration, Instant};

use crate::proxy;

/// ENDPOINT_ENV overrides the endpoint of the local API, like the clients of
/// the daemon do.
const ENDPOINT_ENV: &str = "UNISON_LOCAL_API";
/// BINARY_ENV points at the daemon binary of a development build.
const BINARY_ENV: &str = "UNISON_DAEMON_BIN";

/// Limits of the check and the startup of the daemon.
const PROBE_TIMEOUT: Duration = Duration::from_millis(500);
const START_TIMEOUT: Duration = Duration::from_secs(10);
const START_INTERVAL: Duration = Duration::from_millis(200);
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

/// resolve_endpoint returns the endpoint of the local API.
pub fn resolve_endpoint() -> Result<String, String> {
    if let Some(path) = std::env::var_os(ENDPOINT_ENV) {
        let path = path.to_string_lossy().into_owned();
        if !path.is_empty() {
            return Ok(path);
        }
    }
    platform_endpoint()
}

/// ensure_running starts the daemon when it does not answer on the endpoint. A
/// login item that the system manages owns the daemon, so the app only waits
/// for it.
pub async fn ensure_running(endpoint: &str) -> Result<(), String> {
    if probe(endpoint).await {
        return Ok(());
    }

    match crate::agent::status() {
        crate::agent::LoginItemStatus::Enabled => {
            if wait_for(endpoint, LOGIN_WAIT).await.is_ok() {
                return Ok(());
            }
            // A login item that the system cannot start must not leave the app
            // without a daemon.
            match binary() {
                Some(binary) => {
                    start(&binary)?;
                    wait_for(endpoint, START_TIMEOUT).await
                }
                None => Err(format!(
                    "the login item does not start the daemon on {endpoint}"
                )),
            }
        }
        crate::agent::LoginItemStatus::RequiresApproval => {
            Err("the login item waits for approval in System Settings".to_string())
        }
        _ => match binary() {
            Some(binary) => {
                start(&binary)?;
                wait_for(endpoint, START_TIMEOUT).await
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
/// autostart entry of the system, and from next to the app binary when that
/// entry is turned off. Development builds start the daemon from `bin/`.
pub fn binary() -> Option<PathBuf> {
    if let Some(path) = std::env::var_os(BINARY_ENV) {
        return Some(PathBuf::from(path));
    }
    let path = if cfg!(debug_assertions) {
        PathBuf::from(env!("CARGO_MANIFEST_DIR"))
            .join("../../bin")
            .join(BINARY_NAME)
    } else {
        let exe = std::env::current_exe().ok()?;
        exe.parent()?.join(BINARY_NAME)
    };
    path.is_file().then_some(path)
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

/// start runs the daemon next to the app. The daemon outlives the app, so the
/// child is waited on in the background and never killed.
fn start(binary: &PathBuf) -> Result<(), String> {
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
        let _ = child.wait();
        if let Ok(mut slot) = STARTED.lock()
            && *slot == Some(pid)
        {
            *slot = None;
        }
    });
    Ok(())
}

/// platform_endpoint returns the endpoint the daemon of this user serves.
fn platform_endpoint() -> Result<String, String> {
    #[cfg(unix)]
    {
        let home =
            std::env::var_os("HOME").ok_or_else(|| "the home directory is not set".to_string())?;
        let mut dir = PathBuf::from(home);
        #[cfg(target_os = "macos")]
        dir.push("Library/Application Support/Unison");
        #[cfg(not(target_os = "macos"))]
        dir.push(".local/share/unison");
        dir.push("unisond.sock");
        Ok(dir.to_string_lossy().into_owned())
    }
    #[cfg(windows)]
    {
        windows_endpoint()
    }
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
