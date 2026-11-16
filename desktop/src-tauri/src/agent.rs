//! Autostart of the daemon.
//!
//! The packaged app keeps the daemon in the autostart list of the system, so it
//! runs without the window: a login item that `SMAppService` registers on macOS,
//! and a `Run` entry of the current user on Windows.

use serde::Serialize;

/// PLIST_NAME names the login item inside `Contents/Library/LaunchAgents`.
#[cfg(target_os = "macos")]
pub const PLIST_NAME: &str = "app.unison.unisond.plist";

/// VALUE_NAME names the autostart entry in the `Run` key of the current user.
/// The name is the product name, which the uninstaller uses to remove it.
#[cfg(windows)]
const VALUE_NAME: &str = "Unison";

/// ACTION_ARG selects the login item action on the command line, so a packaged
/// app can report and change the login item without opening a window.
const ACTION_ARG: &str = "--login-item=";

/// LoginItemStatus is the state of the login item in the system.
#[derive(Clone, Copy, PartialEq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum LoginItemStatus {
    /// The platform has no login item for the daemon.
    Unsupported,
    /// The login item is not registered.
    NotRegistered,
    /// The login item is registered and starts the daemon at login.
    Enabled,
    /// The user has to allow the login item in System Settings.
    RequiresApproval,
    /// The system does not find the login item of the bundle.
    NotFound,
}

impl LoginItemStatus {
    /// as_str is the name the command line prints.
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Unsupported => "unsupported",
            Self::NotRegistered => "not-registered",
            Self::Enabled => "enabled",
            Self::RequiresApproval => "requires-approval",
            Self::NotFound => "not-found",
        }
    }
}

/// LoginItem is the answer of the login item commands.
#[derive(Serialize)]
pub struct LoginItem {
    pub status: LoginItemStatus,
}

/// status returns the state of the login item.
pub fn status() -> LoginItemStatus {
    #[cfg(target_os = "macos")]
    {
        platform::status()
    }
    #[cfg(windows)]
    {
        platform::status()
    }
    #[cfg(not(any(target_os = "macos", windows)))]
    {
        LoginItemStatus::Unsupported
    }
}

/// set registers or unregisters the login item and returns its new state.
pub fn set(enabled: bool) -> Result<LoginItemStatus, String> {
    #[cfg(target_os = "macos")]
    {
        if enabled {
            platform::register()?;
        } else {
            platform::unregister()?;
        }
        Ok(platform::status())
    }
    #[cfg(windows)]
    {
        platform::set(enabled)?;
        Ok(platform::status())
    }
    #[cfg(not(any(target_os = "macos", windows)))]
    {
        let _ = enabled;
        Ok(LoginItemStatus::Unsupported)
    }
}

/// register_at_launch registers the login item on the first launch of the
/// packaged app. It leaves a login item that the user has to approve alone. The
/// autostart entry of Windows belongs to the installer, so the toggle of the
/// settings keeps ownership of it.
pub fn register_at_launch() -> Option<Result<LoginItemStatus, String>> {
    if cfg!(debug_assertions) || cfg!(windows) {
        return None;
    }
    match status() {
        LoginItemStatus::NotRegistered | LoginItemStatus::NotFound => Some(set(true)),
        _ => None,
    }
}

/// run_arg handles the login item action of the command line. It returns the
/// exit code, or None when the arguments ask for the regular start.
pub fn run_arg() -> Option<i32> {
    let arg = std::env::args()
        .skip(1)
        .find(|arg| arg.starts_with(ACTION_ARG))?;
    let action = &arg[ACTION_ARG.len()..];
    match action {
        "status" => {
            println!("{}", status().as_str());
            Some(0)
        }
        "register" => Some(report(set(true))),
        "unregister" => Some(report(set(false))),
        other => {
            eprintln!("unison: unknown login item action {other}");
            Some(2)
        }
    }
}

/// report prints the result of a login item change and returns the exit code.
fn report(result: Result<LoginItemStatus, String>) -> i32 {
    match result {
        Ok(current) => {
            println!("{}", current.as_str());
            0
        }
        Err(err) => {
            eprintln!("unison: {err}");
            1
        }
    }
}

/// platform reports and changes the login item through the system.
#[cfg(target_os = "macos")]
mod platform {
    use objc2::msg_send;
    use objc2::rc::Retained;
    use objc2::runtime::{AnyClass, AnyObject};
    use objc2_foundation::{NSError, NSString};

    use super::{LoginItemStatus, PLIST_NAME};

    // Linking the framework makes its classes available to the runtime.
    #[link(name = "ServiceManagement", kind = "framework")]
    unsafe extern "C" {}

    /// AGENT_STATUS_* are the values of `SMAppServiceStatus`.
    const AGENT_STATUS_NOT_REGISTERED: isize = 0;
    const AGENT_STATUS_ENABLED: isize = 1;
    const AGENT_STATUS_REQUIRES_APPROVAL: isize = 2;
    const AGENT_STATUS_NOT_FOUND: isize = 3;

    /// class returns the login item class of the system.
    fn class() -> Option<&'static AnyClass> {
        AnyClass::get(c"SMAppService")
    }

    /// agent returns the login item of the bundle, or an error when the system
    /// does not know it.
    fn agent(class: &AnyClass) -> Result<Retained<AnyObject>, String> {
        let name = NSString::from_str(PLIST_NAME);
        // SAFETY: the class is SMAppService and the method returns an instance.
        let service: Option<Retained<AnyObject>> =
            unsafe { msg_send![class, agentServiceWithPlistName: &*name] };
        service.ok_or_else(|| format!("the bundle has no login item {PLIST_NAME}"))
    }

    /// status reads the state of the login item.
    pub fn status() -> LoginItemStatus {
        let Some(class) = class() else {
            return LoginItemStatus::Unsupported;
        };
        let service = match agent(class) {
            Ok(service) => service,
            Err(_) => return LoginItemStatus::NotFound,
        };
        // SAFETY: SMAppService answers -status with an integer.
        let value: isize = unsafe { msg_send![&*service, status] };
        match value {
            AGENT_STATUS_NOT_REGISTERED => LoginItemStatus::NotRegistered,
            AGENT_STATUS_ENABLED => LoginItemStatus::Enabled,
            AGENT_STATUS_REQUIRES_APPROVAL => LoginItemStatus::RequiresApproval,
            AGENT_STATUS_NOT_FOUND => LoginItemStatus::NotFound,
            _ => LoginItemStatus::Unsupported,
        }
    }

    /// register adds the login item to the system.
    pub fn register() -> Result<(), String> {
        let class = class().ok_or_else(|| "the system has no login items".to_string())?;
        let service = agent(class)?;
        change(&service, true)
    }

    /// unregister removes the login item from the system.
    pub fn unregister() -> Result<(), String> {
        let class = class().ok_or_else(|| "the system has no login items".to_string())?;
        let service = agent(class)?;
        change(&service, false)
    }

    /// change calls the registering or unregistering method of the agent.
    fn change(service: &AnyObject, register: bool) -> Result<(), String> {
        let mut error: *mut NSError = std::ptr::null_mut();
        // SAFETY: both methods take an NSError pointer and return a BOOL.
        let ok: bool = unsafe {
            if register {
                msg_send![service, registerAndReturnError: &mut error]
            } else {
                msg_send![service, unregisterAndReturnError: &mut error]
            }
        };
        if ok {
            return Ok(());
        }
        // SAFETY: the system fills the error of a failed call.
        Err(unsafe { error_text(error) })
    }

    /// error_text describes the error the system returned.
    unsafe fn error_text(error: *mut NSError) -> String {
        if error.is_null() {
            return "the login item could not be changed".to_string();
        }
        // SAFETY: the caller passes the error of the call above.
        let description: Option<Retained<NSString>> =
            unsafe { msg_send![&*error, localizedDescription] };
        description
            .map(|text| text.to_string())
            .unwrap_or_else(|| "the login item could not be changed".to_string())
    }
}

/// platform keeps the autostart entry of the daemon in the registry of the
/// current user.
#[cfg(windows)]
mod platform {
    use std::ffi::OsStr;
    use std::os::windows::ffi::OsStrExt;
    use std::path::Path;

    use windows_sys::Win32::Foundation::{ERROR_FILE_NOT_FOUND, ERROR_SUCCESS};
    use windows_sys::Win32::System::Registry::{
        HKEY, HKEY_CURRENT_USER, KEY_QUERY_VALUE, KEY_SET_VALUE, REG_SZ, RegCloseKey,
        RegDeleteValueW, RegOpenKeyExW, RegQueryValueExW, RegSetValueExW,
    };

    use super::{LoginItemStatus, VALUE_NAME};

    /// RUN_KEY holds the programs the system starts at the login of the user.
    const RUN_KEY: &str = r"Software\Microsoft\Windows\CurrentVersion\Run";

    /// Key closes its handle when it leaves the scope.
    struct Key(HKEY);

    impl Drop for Key {
        fn drop(&mut self) {
            // SAFETY: the handle comes from open and is not used afterwards.
            unsafe { RegCloseKey(self.0) };
        }
    }

    /// status reports whether the `Run` entry starts the daemon of this
    /// installation.
    pub fn status() -> LoginItemStatus {
        let Some(command) = read() else {
            return LoginItemStatus::NotRegistered;
        };
        let path = command.trim().trim_matches('"');
        if Path::new(path).is_file() {
            LoginItemStatus::Enabled
        } else {
            LoginItemStatus::NotFound
        }
    }

    /// set writes or removes the `Run` entry of the daemon.
    pub fn set(enabled: bool) -> Result<(), String> {
        if !enabled {
            return remove();
        }
        let binary = crate::daemon::binary()
            .ok_or_else(|| "the daemon is not installed next to the app".to_string())?;
        // The command is the quoted path of the daemon, without arguments.
        let command: Vec<u16> = std::iter::once(u16::from(b'"'))
            .chain(binary.as_os_str().encode_wide())
            .chain(std::iter::once(u16::from(b'"')))
            .chain(std::iter::once(0))
            .collect();
        write(&command)
    }

    /// read returns the command of the `Run` entry, or None when the entry does
    /// not exist.
    fn read() -> Option<String> {
        let key = open(KEY_QUERY_VALUE)?;
        let name = wide(VALUE_NAME);
        let mut size = 0u32;
        // SAFETY: the call only asks for the size of the value.
        let code = unsafe {
            RegQueryValueExW(
                key.0,
                name.as_ptr(),
                std::ptr::null(),
                std::ptr::null_mut(),
                std::ptr::null_mut(),
                &mut size,
            )
        };
        if code != ERROR_SUCCESS || size == 0 {
            return None;
        }
        let mut data = vec![0u8; size as usize];
        // SAFETY: the buffer is as large as the size the call reported.
        let code = unsafe {
            RegQueryValueExW(
                key.0,
                name.as_ptr(),
                std::ptr::null(),
                std::ptr::null_mut(),
                data.as_mut_ptr(),
                &mut size,
            )
        };
        if code != ERROR_SUCCESS {
            return None;
        }
        Some(text(&data))
    }

    /// write stores the command of the `Run` entry.
    fn write(command: &[u16]) -> Result<(), String> {
        let key =
            open(KEY_SET_VALUE).ok_or_else(|| "the autostart key cannot be opened".to_string())?;
        let name = wide(VALUE_NAME);
        let bytes: Vec<u8> = command.iter().flat_map(|unit| unit.to_le_bytes()).collect();
        // SAFETY: the name and the data are null terminated strings of the
        // reported lengths.
        let code = unsafe {
            RegSetValueExW(
                key.0,
                name.as_ptr(),
                0,
                REG_SZ,
                bytes.as_ptr(),
                bytes.len() as u32,
            )
        };
        if code != ERROR_SUCCESS {
            return Err(format!(
                "the autostart entry cannot be written, error {code}"
            ));
        }
        Ok(())
    }

    /// remove deletes the `Run` entry of the daemon.
    fn remove() -> Result<(), String> {
        let Some(key) = open(KEY_SET_VALUE) else {
            return Ok(());
        };
        let name = wide(VALUE_NAME);
        // SAFETY: the name is a null terminated string.
        let code = unsafe { RegDeleteValueW(key.0, name.as_ptr()) };
        if code == ERROR_SUCCESS || code == ERROR_FILE_NOT_FOUND {
            return Ok(());
        }
        Err(format!(
            "the autostart entry cannot be removed, error {code}"
        ))
    }

    /// open opens the `Run` key of the current user.
    fn open(access: u32) -> Option<Key> {
        let path = wide(RUN_KEY);
        let mut key: HKEY = std::ptr::null_mut();
        // SAFETY: the path is a null terminated string and the handle is
        // written by the call.
        let code = unsafe { RegOpenKeyExW(HKEY_CURRENT_USER, path.as_ptr(), 0, access, &mut key) };
        (code == ERROR_SUCCESS).then_some(Key(key))
    }

    /// text decodes a `REG_SZ` value.
    fn text(data: &[u8]) -> String {
        let units: Vec<u16> = data
            .as_chunks::<2>()
            .0
            .iter()
            .map(|unit| u16::from_le_bytes(*unit))
            .collect();
        let end = units
            .iter()
            .position(|unit| *unit == 0)
            .unwrap_or(units.len());
        String::from_utf16_lossy(&units[..end])
    }

    /// wide returns the UTF-16 form of a string with a terminating null.
    fn wide(value: &str) -> Vec<u16> {
        OsStr::new(value)
            .encode_wide()
            .chain(std::iter::once(0))
            .collect()
    }
}
