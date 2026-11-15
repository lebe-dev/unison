//! Discovery and startup of the daemon.

use std::path::PathBuf;
use std::process::{Command, Stdio};
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

/// ensure_running starts the daemon when it does not answer on the endpoint.
pub async fn ensure_running(endpoint: &str) -> Result<(), String> {
    if probe(endpoint).await {
        return Ok(());
    }

    let binary = daemon_binary().ok_or_else(|| "the daemon is not running".to_string())?;
    start(&binary)?;

    let deadline = Instant::now() + START_TIMEOUT;
    while Instant::now() < deadline {
        tokio::time::sleep(START_INTERVAL).await;
        if probe(endpoint).await {
            return Ok(());
        }
    }
    Err(format!("the daemon did not answer on {endpoint}"))
}

/// probe reports whether a process answers on the endpoint.
async fn probe(endpoint: &str) -> bool {
    matches!(
        tokio::time::timeout(PROBE_TIMEOUT, proxy::connect(endpoint)).await,
        Ok(Ok(_))
    )
}

/// daemon_binary returns the daemon to start. Packaged builds start the daemon
/// from the login item of the user, so only development builds spawn it.
fn daemon_binary() -> Option<PathBuf> {
    if let Some(path) = std::env::var_os(BINARY_ENV) {
        return Some(PathBuf::from(path));
    }
    if !cfg!(debug_assertions) {
        return None;
    }
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../bin/unisond");
    path.is_file().then_some(path)
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
    std::thread::spawn(move || {
        let _ = child.wait();
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
