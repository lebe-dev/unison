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
#[derive(Clone, Copy, Debug, PartialEq, Serialize)]
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
    needs_registration(status()).then(|| set(true))
}

/// needs_registration reports whether the launch registers a login item in the
/// given state. A login item that waits for approval or that is enabled is
/// left as the user set it.
fn needs_registration(current: LoginItemStatus) -> bool {
    matches!(
        current,
        LoginItemStatus::NotRegistered | LoginItemStatus::NotFound
    )
}

/// Action is a login item action of the command line.
#[derive(Debug, PartialEq)]
enum Action {
    Status,
    Register,
    Unregister,
}

/// parse_action finds the login item action among the arguments that follow
/// the program name. It returns None when no argument asks for one, and the
/// unknown action as an error.
fn parse_action(args: impl IntoIterator<Item = String>) -> Option<Result<Action, String>> {
    let arg = args.into_iter().find(|arg| arg.starts_with(ACTION_ARG))?;
    let action = match &arg[ACTION_ARG.len()..] {
        "status" => Action::Status,
        "register" => Action::Register,
        "unregister" => Action::Unregister,
        other => return Some(Err(other.to_string())),
    };
    Some(Ok(action))
}

/// run_arg handles the login item action of the command line. It returns the
/// exit code, or None when the arguments ask for the regular start.
pub fn run_arg() -> Option<i32> {
    let action = match parse_action(std::env::args().skip(1))? {
        Ok(action) => action,
        Err(other) => {
            eprintln!("unison: unknown login item action {other}");
            return Some(2);
        }
    };
    let code = match action {
        Action::Status => {
            println!("{}", status().as_str());
            0
        }
        Action::Register => report(set(true)),
        Action::Unregister => report(set(false)),
    };
    Some(code)
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

#[cfg(test)]
mod tests {
    use super::*;

    const ALL: [LoginItemStatus; 5] = [
        LoginItemStatus::Unsupported,
        LoginItemStatus::NotRegistered,
        LoginItemStatus::Enabled,
        LoginItemStatus::RequiresApproval,
        LoginItemStatus::NotFound,
    ];

    fn args(list: &[&str]) -> Vec<String> {
        list.iter().map(|arg| arg.to_string()).collect()
    }

    #[test]
    fn parse_action_reads_the_known_actions() {
        assert_eq!(
            parse_action(args(&["--login-item=status"])),
            Some(Ok(Action::Status))
        );
        assert_eq!(
            parse_action(args(&["--verbose", "--login-item=register"])),
            Some(Ok(Action::Register))
        );
        assert_eq!(
            parse_action(args(&["--login-item=unregister"])),
            Some(Ok(Action::Unregister))
        );
    }

    #[test]
    fn parse_action_leaves_the_regular_start_alone() {
        assert_eq!(parse_action(args(&[])), None);
        assert_eq!(
            parse_action(args(&["--verbose", "login-item=status"])),
            None
        );
    }

    #[test]
    fn parse_action_refuses_an_unknown_action() {
        assert_eq!(
            parse_action(args(&["--login-item=enable"])),
            Some(Err("enable".to_string()))
        );
        assert_eq!(
            parse_action(args(&["--login-item="])),
            Some(Err(String::new()))
        );
        assert_eq!(
            parse_action(args(&["--login-item=Status"])),
            Some(Err("Status".to_string())),
            "the action is case sensitive"
        );
    }

    #[test]
    fn report_returns_the_exit_code() {
        assert_eq!(report(Ok(LoginItemStatus::Enabled)), 0);
        assert_eq!(report(Err("denied".to_string())), 1);
    }

    #[test]
    fn only_a_missing_login_item_is_registered_at_launch() {
        for status in ALL {
            let expected = matches!(
                status,
                LoginItemStatus::NotRegistered | LoginItemStatus::NotFound
            );
            assert_eq!(needs_registration(status), expected, "{status:?}");
        }
    }

    #[test]
    fn status_names_match_the_window_and_the_command_line() {
        let names: Vec<(String, &str)> = ALL
            .iter()
            .map(|status| (serde_json::to_string(status).unwrap(), status.as_str()))
            .collect();
        assert_eq!(
            names,
            [
                (r#""unsupported""#.to_string(), "unsupported"),
                (r#""not_registered""#.to_string(), "not-registered"),
                (r#""enabled""#.to_string(), "enabled"),
                (r#""requires_approval""#.to_string(), "requires-approval"),
                (r#""not_found""#.to_string(), "not-found"),
            ]
        );
        assert_eq!(
            serde_json::to_string(&LoginItem {
                status: LoginItemStatus::Enabled
            })
            .unwrap(),
            r#"{"status":"enabled"}"#
        );
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
