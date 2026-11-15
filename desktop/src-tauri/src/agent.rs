//! Login item of the daemon.
//!
//! The packaged app carries the daemon as a login item and registers it with
//! `SMAppService`, so the system starts the daemon at login. The login item is
//! the property list in `Contents/Library/LaunchAgents`.

use serde::Serialize;

/// PLIST_NAME names the login item inside `Contents/Library/LaunchAgents`.
pub const PLIST_NAME: &str = "app.unison.unisond.plist";

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
    #[cfg(not(target_os = "macos"))]
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
    #[cfg(not(target_os = "macos"))]
    {
        let _ = enabled;
        Ok(LoginItemStatus::Unsupported)
    }
}

/// register_at_launch registers the login item on the first launch of the
/// packaged app. It leaves a login item that the user has to approve alone.
pub fn register_at_launch() -> Option<Result<LoginItemStatus, String>> {
    if cfg!(debug_assertions) {
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
