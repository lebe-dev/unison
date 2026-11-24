//! Shared Windows system helpers for elevated execution and system tools.

use std::ffi::OsString;
use std::path::PathBuf;

/// DEFAULT_SYSTEM_ROOT is the fallback Windows directory when `SystemRoot` is
/// absent or empty.
const DEFAULT_SYSTEM_ROOT: &str = r"C:\Windows";

/// system32_binary returns the absolute path of a binary inside `System32`.
/// Callers must not resolve system tools through `PATH` or the working
/// directory.
pub fn system32_binary(system_root: Option<OsString>, name: &str) -> PathBuf {
    let root = system_root
        .filter(|root| !root.is_empty())
        .unwrap_or_else(|| OsString::from(DEFAULT_SYSTEM_ROOT));
    PathBuf::from(root).join("System32").join(name)
}

#[cfg(windows)]
mod platform {
    use std::ffi::OsStr;
    use std::os::windows::ffi::OsStrExt;
    use std::os::windows::process::CommandExt;
    use std::path::{Path, PathBuf};
    use std::process::{Command, Stdio};

    use windows_sys::Win32::Foundation::{CloseHandle, ERROR_CANCELLED, GetLastError, HANDLE};
    use windows_sys::Win32::System::Com::{
        COINIT_APARTMENTTHREADED, CoInitializeEx, CoUninitialize,
    };
    use windows_sys::Win32::System::Threading::{
        CREATE_NO_WINDOW, GetExitCodeProcess, INFINITE, WaitForSingleObject,
    };
    use windows_sys::Win32::UI::Shell::{
        SEE_MASK_NOCLOSEPROCESS, SHELLEXECUTEINFOW, ShellExecuteExW,
    };

    use super::system32_binary;

    /// HIDE is the `SW_HIDE` value of the window state.
    const HIDE: i32 = 0;

    /// OwnedHandle closes a Win32 handle when dropped.
    pub struct OwnedHandle(HANDLE);

    impl OwnedHandle {
        /// new wraps a non-null handle and returns None for a null handle.
        pub fn new(handle: HANDLE) -> Option<Self> {
            (!handle.is_null()).then_some(Self(handle))
        }

        /// as_raw returns the underlying Win32 handle.
        pub fn as_raw(&self) -> HANDLE {
            self.0
        }
    }

    impl Drop for OwnedHandle {
        fn drop(&mut self) {
            // SAFETY: the handle is owned by this wrapper and is not used after
            // drop.
            unsafe { CloseHandle(self.0) };
        }
    }

    /// ComGuard uninitializes COM on drop when this thread initialized it.
    struct ComGuard(bool);

    impl ComGuard {
        fn init_sta() -> Self {
            // SAFETY: CoInitializeEx takes only a reserved null pointer and the
            // apartment model flag.
            let initialized =
                unsafe { CoInitializeEx(std::ptr::null(), COINIT_APARTMENTTHREADED as u32) } >= 0;
            Self(initialized)
        }
    }

    impl Drop for ComGuard {
        fn drop(&mut self) {
            if self.0 {
                // SAFETY: this thread initialized COM in init_sta and does not
                // use it afterwards.
                unsafe { CoUninitialize() };
            }
        }
    }

    /// ElevateError describes why an elevated process could not be run or
    /// waited on.
    #[derive(Debug, PartialEq, Eq)]
    pub enum ElevateError {
        /// The user cancelled the UAC elevation prompt.
        Cancelled,
        /// `ShellExecuteExW` failed with the given Win32 error code.
        Start(u32),
        /// `ShellExecuteExW` succeeded but returned no process handle.
        NoProcessHandle,
        /// `GetExitCodeProcess` failed after the process exited.
        ReadExitCode,
    }

    /// wide returns the UTF-16 form of a string with a terminating null.
    pub fn wide(value: &str) -> Vec<u16> {
        OsStr::new(value)
            .encode_wide()
            .chain(std::iter::once(0))
            .collect()
    }

    /// system32 returns the absolute path of a binary in the system directory.
    pub fn system32(name: &str) -> PathBuf {
        system32_binary(std::env::var_os("SystemRoot"), name)
    }

    /// run_system_tool runs a binary from `System32` without a console window
    /// and returns its exit code.
    pub fn run_system_tool<I, S>(name: &str, args: I) -> Result<i32, String>
    where
        I: IntoIterator<Item = S>,
        S: AsRef<OsStr>,
    {
        let status = Command::new(system32(name))
            .args(args)
            .creation_flags(CREATE_NO_WINDOW)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status()
            .map_err(|err| format!("cannot run {name}: {err}"))?;
        Ok(status.code().unwrap_or(-1))
    }

    /// run_elevated starts a program with the `runas` verb, waits for it to
    /// exit and returns its exit code.
    pub fn run_elevated(program: &Path, parameters: &str) -> Result<i32, ElevateError> {
        // The shell executes the verb through COM, so the calling thread
        // initializes an apartment first.
        let _com = ComGuard::init_sta();
        let file = wide(&program.to_string_lossy());
        let verb = wide("runas");
        let params = wide(parameters);
        let mut info = SHELLEXECUTEINFOW {
            cbSize: std::mem::size_of::<SHELLEXECUTEINFOW>() as u32,
            fMask: SEE_MASK_NOCLOSEPROCESS,
            lpVerb: verb.as_ptr(),
            lpFile: file.as_ptr(),
            lpParameters: params.as_ptr(),
            nShow: HIDE,
            ..Default::default()
        };
        // SAFETY: the wide buffers stay alive until ShellExecuteExW returns and
        // hProcess is wrapped in OwnedHandle below.
        if unsafe { ShellExecuteExW(&mut info) } == 0 {
            let code = unsafe { GetLastError() };
            if code == ERROR_CANCELLED {
                return Err(ElevateError::Cancelled);
            }
            return Err(ElevateError::Start(code));
        }
        let process = OwnedHandle::new(info.hProcess).ok_or(ElevateError::NoProcessHandle)?;
        // SAFETY: process holds a valid handle returned by ShellExecuteExW.
        unsafe {
            WaitForSingleObject(process.as_raw(), INFINITE);
            let mut code = 0u32;
            if GetExitCodeProcess(process.as_raw(), &mut code) == 0 {
                return Err(ElevateError::ReadExitCode);
            }
            Ok(code as i32)
        }
    }
}

#[cfg(windows)]
pub use platform::{ElevateError, OwnedHandle, run_elevated, run_system_tool, wide};

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn system32_binary_resolves_in_the_system_directory() {
        assert_eq!(
            system32_binary(Some(r"D:\Windows".into()), "netsh.exe"),
            PathBuf::from(r"D:\Windows")
                .join("System32")
                .join("netsh.exe")
        );
        assert_eq!(
            system32_binary(None, "taskkill.exe"),
            PathBuf::from(r"C:\Windows")
                .join("System32")
                .join("taskkill.exe")
        );
        assert_eq!(
            system32_binary(Some(OsString::new()), "taskkill.exe"),
            PathBuf::from(r"C:\Windows")
                .join("System32")
                .join("taskkill.exe"),
            "an empty root turned into a relative path"
        );
    }
}
