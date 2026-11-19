//! Installation of the daemon from the app.
//!
//! The installer puts the daemon into the program files, writes the startup
//! entry of the current user and adds the firewall rule. The app repeats the
//! same steps when the installation is missing, so a repair needs no terminal:
//! it runs a hidden mode of itself once with an elevation prompt, and starts the
//! daemon as the user afterwards.

use std::ffi::OsString;
#[cfg(any(windows, test))]
use std::path::Path;
use std::path::PathBuf;

use serde::Serialize;

/// ARG selects the installation of the daemon on the command line.
pub const ARG: &str = "--install-daemon";
/// SOURCE_ARG names the daemon to install. The app passes it, so the elevated
/// process does not depend on the environment of the app.
#[cfg(any(windows, test))]
const SOURCE_ARG: &str = "--daemon-source=";
/// LOG_ARG names the file the hidden mode reports its steps to, since an
/// elevated process has no console the app could read.
#[cfg(any(windows, test))]
const LOG_ARG: &str = "--log=";
/// LOG_NAME names the report of the hidden mode in the temporary directory.
#[cfg(windows)]
const LOG_NAME: &str = "unison-daemon-install.log";

/// PRODUCT is the name of the product: the folder in the program files and the
/// name of the startup entry.
pub const PRODUCT: &str = "Unison";
/// BINARY is the daemon, next to the app and in the installation directory.
pub const BINARY: &str = "unisond.exe";
/// RUN_KEY holds the programs the system starts at the login of the user.
pub const RUN_KEY: &str = r"Software\Microsoft\Windows\CurrentVersion\Run";
/// RUN_VALUE names the startup entry of the daemon.
pub const RUN_VALUE: &str = PRODUCT;
/// FIREWALL_RULE names the rule of the daemon in Windows Firewall.
pub const FIREWALL_RULE: &str = "Unison daemon";
/// FIREWALL_PROFILES keeps the rule off the public networks, where the daemon
/// neither listens nor discovers.
pub const FIREWALL_PROFILES: &str = "private,domain";
/// FIREWALL_PROTOCOL is the protocol the daemon listens on.
pub const FIREWALL_PROTOCOL: &str = "UDP";

/// Offer tells the window whether it can install the daemon.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Offer {
    /// The app installs no daemon here.
    Off,
    /// The daemon is missing and the app has a copy to install.
    Available,
    /// The daemon is missing and the app has no copy of it.
    Missing,
}

/// offer returns whether the app can install the daemon: only Windows does, and
/// only while the installation is not in place.
pub fn offer() -> Offer {
    #[cfg(windows)]
    {
        if crate::agent::status() == crate::agent::LoginItemStatus::Enabled {
            return Offer::Off;
        }
        if source().is_ok() {
            return Offer::Available;
        }
        Offer::Missing
    }
    #[cfg(not(windows))]
    {
        Offer::Off
    }
}

/// source returns the daemon to install: the one next to the app, then the
/// override of a development build.
pub fn source() -> Result<PathBuf, String> {
    source_from(next_to_app(), std::env::var_os(crate::daemon::BINARY_ENV))
}

/// next_to_app returns the daemon of the directory of the app.
fn next_to_app() -> Option<PathBuf> {
    let exe = std::env::current_exe().ok()?;
    Some(exe.parent()?.join(BINARY))
}

/// source_from returns the daemon the override names, or the one next to the
/// app when it exists.
fn source_from(next_to_app: Option<PathBuf>, custom: Option<OsString>) -> Result<PathBuf, String> {
    if let Some(path) = next_to_app.filter(|path| path.is_file()) {
        return Ok(path);
    }
    if let Some(path) = custom.filter(|path| !path.is_empty()) {
        let path = PathBuf::from(path);
        if path.is_file() {
            return Ok(path);
        }
        return Err(format!("the daemon {} does not exist", path.display()));
    }
    Err(format!(
        "the daemon {BINARY} is missing next to the app; reinstall Unison"
    ))
}

/// install_dir returns the directory of the installer, or the directory of the
/// app when it already runs from there.
#[cfg(any(windows, test))]
fn install_dir(exe_dir: &Path, program_files: Option<OsString>) -> Result<PathBuf, String> {
    let dir = installed_dir(program_files)
        .ok_or_else(|| "the program files directory is not set".to_string())?;
    if same_path(exe_dir, &dir) {
        return Ok(exe_dir.to_path_buf());
    }
    Ok(dir)
}

/// installed_dir returns the directory of the installer, or None when the
/// system names no program files.
#[cfg(any(windows, test))]
pub(crate) fn installed_dir(program_files: Option<OsString>) -> Option<PathBuf> {
    let root = program_files?;
    if root.is_empty() {
        return None;
    }
    Some(PathBuf::from(root).join(PRODUCT))
}

/// same_path compares two paths the way Windows does: without the case, without
/// a trailing separator and with either separator.
#[cfg(any(windows, test))]
fn same_path(left: &Path, right: &Path) -> bool {
    let normalize = |path: &Path| -> String {
        path.to_string_lossy()
            .trim_end_matches(['\\', '/'])
            .replace('\\', "/")
    };
    normalize(left).eq_ignore_ascii_case(&normalize(right))
}

/// copy puts the daemon into the directory through a temporary file and a
/// rename, so a failed copy leaves the installed daemon alone.
#[cfg(any(windows, test))]
fn copy(source: &Path, target: &Path) -> Result<(), String> {
    // An app that already runs from the installation directory has nothing to
    // copy, and its daemon may hold the target file open.
    if same_path(source, target) {
        return Ok(());
    }
    let dir = target
        .parent()
        .ok_or_else(|| "the installation directory is unknown".to_string())?;
    std::fs::create_dir_all(dir)
        .map_err(|err| format!("cannot create {}: {err}", dir.display()))?;
    let temp = target.with_extension("exe.tmp");
    std::fs::copy(source, &temp)
        .map_err(|err| format!("cannot copy the daemon to {}: {err}", temp.display()))?;
    std::fs::rename(&temp, target).map_err(|err| {
        let _ = std::fs::remove_file(&temp);
        format!("cannot replace the daemon {}: {err}", target.display())
    })
}

/// elevated_args returns the parameters of the hidden mode, which the app
/// starts with one elevation prompt.
#[cfg(any(windows, test))]
fn elevated_args(source: &Path, log: &Path) -> String {
    format!(
        "{ARG} {SOURCE_ARG}{} {LOG_ARG}{}",
        quoted(source),
        quoted(log)
    )
}

/// quoted wraps a path in quotes, since the hidden mode receives one command
/// line and the program files carry a space.
#[cfg(any(windows, test))]
fn quoted(path: &Path) -> String {
    format!("\"{}\"", path.display())
}

/// firewall_steps returns the netsh calls of the installation. The old rule
/// goes first, so a repeat does not add a second rule.
#[cfg(any(windows, test))]
fn firewall_steps(binary: &Path) -> [Vec<String>; 2] {
    let delete = vec![
        "advfirewall".to_string(),
        "firewall".to_string(),
        "delete".to_string(),
        "rule".to_string(),
        format!("name={FIREWALL_RULE}"),
    ];
    let add = vec![
        "advfirewall".to_string(),
        "firewall".to_string(),
        "add".to_string(),
        "rule".to_string(),
        format!("name={FIREWALL_RULE}"),
        "dir=in".to_string(),
        "action=allow".to_string(),
        format!("program={}", binary.display()),
        "enable=yes".to_string(),
        format!("profile={FIREWALL_PROFILES}"),
        format!("protocol={FIREWALL_PROTOCOL}"),
    ];
    [delete, add]
}

/// netsh_path returns the absolute path of netsh. An elevated process must not
/// run a program of the directory it starts from.
#[cfg(any(windows, test))]
fn netsh_path(system_root: Option<OsString>) -> PathBuf {
    let root = system_root
        .filter(|root| !root.is_empty())
        .unwrap_or_else(|| OsString::from(r"C:\Windows"));
    PathBuf::from(root).join("System32").join("netsh.exe")
}

/// last_line returns the last line of a report that carries text.
#[cfg(any(windows, test))]
fn last_line(text: &str) -> Option<&str> {
    text.lines().map(str::trim).rfind(|line| !line.is_empty())
}

/// reason_from_log returns the message of the last line of the report of the
/// hidden mode.
#[cfg(any(windows, test))]
fn reason_from_log(text: &str) -> Option<String> {
    let line = last_line(text)?;
    Some(line.strip_prefix("unison: ").unwrap_or(line).to_string())
}

/// Options are the arguments of the hidden mode.
#[cfg(any(windows, test))]
#[derive(Debug, Default, PartialEq)]
struct Options {
    /// Source names the daemon the app passed, absent when the hidden mode runs
    /// on its own.
    source: Option<PathBuf>,
    /// Log names the file the hidden mode reports its steps to.
    log: Option<PathBuf>,
}

/// options_from reads the arguments of the hidden mode.
#[cfg(any(windows, test))]
fn options_from(args: impl IntoIterator<Item = OsString>) -> Options {
    let mut options = Options::default();
    for arg in args {
        let text = arg.to_string_lossy();
        if let Some(path) = text.strip_prefix(SOURCE_ARG) {
            options.source = Some(PathBuf::from(path));
        } else if let Some(path) = text.strip_prefix(LOG_ARG) {
            options.log = Some(PathBuf::from(path));
        }
    }
    options
}

/// run_arg handles the installation of the daemon on the command line. It
/// returns the exit code, or None when the arguments ask for the regular start.
pub fn run_arg() -> Option<i32> {
    if !requested(std::env::args_os().skip(1)) {
        return None;
    }
    #[cfg(windows)]
    {
        Some(platform::hidden(options_from(std::env::args_os().skip(1))))
    }
    #[cfg(not(windows))]
    {
        eprintln!("unison: the daemon can be installed from the app only on Windows");
        Some(2)
    }
}

/// requested reports whether the arguments ask to install the daemon.
fn requested(args: impl IntoIterator<Item = OsString>) -> bool {
    args.into_iter().any(|arg| arg == ARG)
}

/// install installs the daemon with one elevation and starts nothing: the app
/// starts the daemon as the user afterwards.
pub fn install() -> Result<(), String> {
    #[cfg(windows)]
    {
        platform::install()
    }
    #[cfg(not(windows))]
    {
        Err("the daemon can be installed from the app only on Windows".to_string())
    }
}

/// platform installs the daemon on Windows.
#[cfg(windows)]
mod platform {
    use std::ffi::OsStr;
    use std::io::Write;
    use std::os::windows::ffi::OsStrExt;
    use std::os::windows::process::CommandExt;
    use std::path::{Path, PathBuf};
    use std::process::{Command, Stdio};

    use windows_sys::Win32::Foundation::{CloseHandle, ERROR_CANCELLED, GetLastError};
    use windows_sys::Win32::System::Com::{
        COINIT_APARTMENTTHREADED, CoInitializeEx, CoUninitialize,
    };
    use windows_sys::Win32::System::Threading::{
        GetExitCodeProcess, INFINITE, WaitForSingleObject,
    };
    use windows_sys::Win32::UI::Shell::{
        SEE_MASK_NOCLOSEPROCESS, SHELLEXECUTEINFOW, ShellExecuteExW,
    };

    use super::*;

    /// NO_WINDOW keeps the console programs of the installation from opening a
    /// window of their own.
    const NO_WINDOW: u32 = 0x0800_0000;
    /// HIDE is the `SW_HIDE` value of the window state.
    const HIDE: i32 = 0;

    /// hidden runs the steps of the installation and returns the exit code.
    pub fn hidden(options: Options) -> i32 {
        match install_daemon(&options) {
            Ok(dir) => {
                report(
                    &options,
                    &format!("unison: the daemon is installed in {}", dir.display()),
                );
                0
            }
            Err(err) => {
                report(&options, &format!("unison: {err}"));
                1
            }
        }
    }

    /// install_daemon copies the daemon, writes the startup entry and adds the
    /// firewall rule.
    fn install_daemon(options: &Options) -> Result<PathBuf, String> {
        let source = source_from(
            next_to_app(),
            options
                .source
                .clone()
                .map(PathBuf::into_os_string)
                .or_else(|| std::env::var_os(crate::daemon::BINARY_ENV)),
        )?;
        let exe = std::env::current_exe().map_err(|err| format!("cannot find the app: {err}"))?;
        let exe_dir = exe
            .parent()
            .ok_or_else(|| "the app has no directory".to_string())?;
        let dir = install_dir(exe_dir, std::env::var_os("ProgramFiles"))?;
        let target = dir.join(BINARY);

        report(
            options,
            &format!(
                "unison: installing the daemon from {} into {}",
                source.display(),
                dir.display()
            ),
        );
        copy(&source, &target)?;
        crate::agent::register_binary(&target)?;
        report(
            options,
            &format!("unison: the startup entry {RUN_VALUE} is written"),
        );
        firewall(&target, options)?;
        Ok(dir)
    }

    /// firewall replaces the rule of the daemon.
    fn firewall(binary: &Path, options: &Options) -> Result<(), String> {
        let [delete, add] = firewall_steps(binary);
        // A rule that does not exist yet is the normal first installation.
        let _ = netsh(&delete);
        let code = netsh(&add)?;
        if code != 0 {
            return Err(format!(
                "the firewall rule {FIREWALL_RULE} cannot be added, netsh exited with {code}"
            ));
        }
        report(
            options,
            &format!("unison: the firewall rule {FIREWALL_RULE} is set"),
        );
        Ok(())
    }

    /// netsh runs one netsh call and returns its exit code.
    fn netsh(args: &[String]) -> Result<i32, String> {
        let status = Command::new(netsh_path(std::env::var_os("SystemRoot")))
            .args(args)
            .creation_flags(NO_WINDOW)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status()
            .map_err(|err| format!("cannot run netsh: {err}"))?;
        Ok(status.code().unwrap_or(-1))
    }

    /// report writes one step of the installation to the log file and the
    /// console. A process without a console must not fail on the write.
    fn report(options: &Options, line: &str) {
        let _ = writeln!(std::io::stdout(), "{line}");
        let Some(path) = &options.log else {
            return;
        };
        let file = std::fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(path);
        if let Ok(mut file) = file {
            let _ = writeln!(file, "{line}");
        }
    }

    /// install asks for the elevation once and waits for the hidden mode. The
    /// steps of the hidden mode go to the log of the app.
    pub fn install() -> Result<(), String> {
        let exe = std::env::current_exe().map_err(|err| format!("cannot find the app: {err}"))?;
        let source = super::source()?;
        let log = std::env::temp_dir().join(LOG_NAME);
        let _ = std::fs::remove_file(&log);

        crate::journal::record(
            "INFO",
            "the app asks for the elevation to install the daemon",
        );
        let code = elevate(&exe, &source, &log)?;
        let report = std::fs::read_to_string(&log).unwrap_or_default();
        let _ = std::fs::remove_file(&log);
        for line in report.lines().filter(|line| !line.trim().is_empty()) {
            let line = line.trim();
            let level = if code == 0 { "INFO" } else { "ERROR" };
            crate::journal::record(level, line);
        }
        if code == 0 {
            return Ok(());
        }
        Err(reason_from_log(&report).unwrap_or_else(|| {
            format!("the daemon was not installed, the hidden mode exited with {code}")
        }))
    }

    /// elevate runs the hidden mode of the app with the elevation prompt and
    /// returns its exit code.
    fn elevate(exe: &Path, source: &Path, log: &Path) -> Result<i32, String> {
        // The shell runs the verb through COM, so the calling thread
        // initializes it first.
        // SAFETY: the call takes no other arguments than the model.
        let initialized =
            unsafe { CoInitializeEx(std::ptr::null(), COINIT_APARTMENTTHREADED as u32) } >= 0;
        let result = elevate_with(exe, source, log);
        if initialized {
            // SAFETY: the thread initialized COM above and does not use it
            // afterwards.
            unsafe { CoUninitialize() };
        }
        result
    }

    /// elevate_with starts the hidden mode and waits for its exit code.
    fn elevate_with(exe: &Path, source: &Path, log: &Path) -> Result<i32, String> {
        let file = wide(&exe.to_string_lossy());
        let verb = wide("runas");
        let params = wide(&elevated_args(source, log));
        let mut info = SHELLEXECUTEINFOW {
            cbSize: std::mem::size_of::<SHELLEXECUTEINFOW>() as u32,
            fMask: SEE_MASK_NOCLOSEPROCESS,
            lpVerb: verb.as_ptr(),
            lpFile: file.as_ptr(),
            lpParameters: params.as_ptr(),
            nShow: HIDE,
            ..Default::default()
        };
        // SAFETY: the strings live until the call returns and the process
        // handle is closed below.
        if unsafe { ShellExecuteExW(&mut info) } == 0 {
            let code = unsafe { GetLastError() };
            if code == ERROR_CANCELLED {
                return Err(
                    "the installation was cancelled in the elevation prompt; nothing was changed"
                        .to_string(),
                );
            }
            return Err(format!("cannot start the installation: error {code}"));
        }
        if info.hProcess.is_null() {
            return Err("cannot wait for the installation".to_string());
        }
        // SAFETY: the handle comes from the call above and is closed here.
        unsafe {
            WaitForSingleObject(info.hProcess, INFINITE);
            let mut code = 0u32;
            let ok = GetExitCodeProcess(info.hProcess, &mut code);
            CloseHandle(info.hProcess);
            if ok == 0 {
                return Err("cannot read the result of the installation".to_string());
            }
            Ok(code as i32)
        }
    }

    /// wide returns the UTF-16 form of a string with a terminating null.
    fn wide(value: &str) -> Vec<u16> {
        OsStr::new(value)
            .encode_wide()
            .chain(std::iter::once(0))
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// HOOKS is the installer script that writes the same names.
    const HOOKS: &str = include_str!("../installer-hooks.nsh");

    /// TempDir is a directory of a test that removes itself.
    struct TempDir(PathBuf);

    impl TempDir {
        fn new() -> Self {
            use std::sync::atomic::{AtomicUsize, Ordering};
            static NEXT: AtomicUsize = AtomicUsize::new(0);
            let name = format!(
                "unison-install-{}-{}",
                std::process::id(),
                NEXT.fetch_add(1, Ordering::Relaxed)
            );
            let path = std::env::temp_dir().join(name);
            let _ = std::fs::remove_dir_all(&path);
            std::fs::create_dir_all(&path).expect("create the directory");
            Self(path)
        }

        fn join(&self, name: &str) -> PathBuf {
            self.0.join(name)
        }
    }

    impl Drop for TempDir {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }

    fn args(list: &[&str]) -> Vec<OsString> {
        list.iter().map(OsString::from).collect()
    }

    #[test]
    fn requested_reads_the_argument_of_the_hidden_mode() {
        assert!(requested(args(&[ARG])));
        assert!(requested(args(&["--verbose", ARG])));
        assert!(!requested(args(&[])));
        assert!(!requested(args(&["--login-item=status"])));
        assert!(
            !requested(args(&["--install-daemon=1"])),
            "an argument with a value was taken for the hidden mode"
        );
    }

    #[test]
    fn options_from_reads_the_paths_of_the_hidden_mode() {
        let options = options_from(args(&[
            ARG,
            r"--daemon-source=C:\Tools\unisond.exe",
            r"--log=C:\Temp\unison-daemon-install.log",
        ]));
        assert_eq!(options.source, Some(PathBuf::from(r"C:\Tools\unisond.exe")));
        assert_eq!(
            options.log,
            Some(PathBuf::from(r"C:\Temp\unison-daemon-install.log"))
        );
    }

    #[test]
    fn options_from_leaves_the_paths_absent() {
        assert_eq!(options_from(args(&[ARG])), Options::default());
        assert_eq!(options_from(args(&[])), Options::default());
    }

    #[test]
    fn source_from_prefers_the_daemon_next_to_the_app() {
        let dir = TempDir::new();
        let next = dir.join(BINARY);
        std::fs::write(&next, b"app").unwrap();
        let other = dir.join("override.exe");
        std::fs::write(&other, b"override").unwrap();

        assert_eq!(
            source_from(Some(next.clone()), Some(other.into_os_string())),
            Ok(next)
        );
    }

    #[test]
    fn source_from_uses_the_override_of_a_development_build() {
        let dir = TempDir::new();
        let other = dir.join("override.exe");
        std::fs::write(&other, b"override").unwrap();

        assert_eq!(
            source_from(
                Some(dir.join("missing.exe")),
                Some(other.clone().into_os_string())
            ),
            Ok(other.clone())
        );
        assert_eq!(
            source_from(None, Some(other.clone().into_os_string())),
            Ok(other)
        );
    }

    #[test]
    fn source_from_reports_a_missing_daemon() {
        let dir = TempDir::new();
        let err = source_from(Some(dir.join("missing.exe")), None)
            .expect_err("a missing daemon was reported as installed");
        assert!(err.contains("reinstall Unison"), "{err}");
        assert!(
            source_from(None, Some(OsString::new())).is_err(),
            "an empty override was taken for a path"
        );
    }

    #[test]
    fn source_from_reports_an_override_that_does_not_exist() {
        let dir = TempDir::new();
        let missing = dir.join("override.exe");
        let err = source_from(None, Some(missing.clone().into_os_string()))
            .expect_err("a missing override was reported as installed");
        assert!(err.contains(&missing.display().to_string()), "{err}");
    }

    #[test]
    fn install_dir_is_the_product_folder_of_the_program_files() {
        assert_eq!(
            install_dir(
                Path::new(r"C:\Users\someone\Downloads\Unison"),
                Some(r"C:\Program Files".into())
            ),
            Ok(PathBuf::from(r"C:\Program Files").join(PRODUCT))
        );
    }

    #[test]
    fn install_dir_keeps_the_folder_of_an_installed_app() {
        let dir = PathBuf::from(r"C:\Program Files").join(PRODUCT);
        assert_eq!(
            install_dir(&dir, Some(r"C:\Program Files".into())),
            Ok(dir.clone())
        );

        // The registry and the environment differ in the case only.
        let exe = Path::new(r"c:\program files\UNISON");
        assert_eq!(
            install_dir(exe, Some(r"C:\Program Files".into())),
            Ok(exe.to_path_buf())
        );
        let sep = std::path::MAIN_SEPARATOR;
        let trailing = PathBuf::from(format!("C:\\Program Files{sep}{PRODUCT}{sep}"));
        assert_eq!(
            install_dir(&trailing, Some(r"C:\Program Files".into())),
            Ok(trailing),
            "a trailing separator broke the comparison"
        );
    }

    #[test]
    fn install_dir_needs_the_program_files() {
        let exe = Path::new(r"C:\Tools\Unison");
        assert!(install_dir(exe, None).is_err());
        assert!(install_dir(exe, Some(OsString::new())).is_err());
    }

    #[test]
    fn elevated_args_pass_the_paths_through_quotes() {
        assert_eq!(
            elevated_args(
                Path::new(r"C:\Program Files\Unison\unisond.exe"),
                Path::new(r"C:\Users\Some One\AppData\Local\Temp\unison-daemon-install.log")
            ),
            concat!(
                r#"--install-daemon --daemon-source="C:\Program Files\Unison\unisond.exe""#,
                r#" --log="C:\Users\Some One\AppData\Local\Temp\unison-daemon-install.log""#
            )
        );
    }

    #[test]
    fn firewall_steps_replace_the_rule_of_the_daemon() {
        let [delete, add] = firewall_steps(Path::new(r"C:\Program Files\Unison\unisond.exe"));
        assert_eq!(
            delete,
            [
                "advfirewall",
                "firewall",
                "delete",
                "rule",
                "name=Unison daemon"
            ]
            .map(String::from)
        );
        assert_eq!(
            add,
            [
                "advfirewall",
                "firewall",
                "add",
                "rule",
                "name=Unison daemon",
                "dir=in",
                "action=allow",
                r"program=C:\Program Files\Unison\unisond.exe",
                "enable=yes",
                "profile=private,domain",
                "protocol=UDP",
            ]
            .map(String::from)
        );
    }

    #[test]
    fn netsh_lives_in_the_system_directory() {
        assert_eq!(
            netsh_path(Some(r"D:\Windows".into())),
            PathBuf::from(r"D:\Windows")
                .join("System32")
                .join("netsh.exe")
        );
        assert_eq!(
            netsh_path(None),
            PathBuf::from(r"C:\Windows")
                .join("System32")
                .join("netsh.exe")
        );
        assert_eq!(
            netsh_path(Some(OsString::new())),
            PathBuf::from(r"C:\Windows")
                .join("System32")
                .join("netsh.exe"),
            "an empty root turned into a relative path"
        );
    }

    #[test]
    fn copy_puts_the_daemon_into_the_installation_directory() {
        let dir = TempDir::new();
        let source = dir.join("next/unisond.exe");
        std::fs::create_dir_all(source.parent().unwrap()).unwrap();
        std::fs::write(&source, b"the daemon").unwrap();
        let target = dir.join("program/unisond.exe");

        copy(&source, &target).expect("install the daemon");

        assert_eq!(std::fs::read(&target).unwrap(), b"the daemon");
        assert!(
            !target.with_extension("exe.tmp").exists(),
            "the temporary file stayed"
        );
    }

    #[test]
    fn copy_replaces_an_older_daemon() {
        let dir = TempDir::new();
        let source = dir.join("next/unisond.exe");
        std::fs::create_dir_all(source.parent().unwrap()).unwrap();
        std::fs::write(&source, b"the new daemon").unwrap();
        let target = dir.join("program/unisond.exe");
        std::fs::create_dir_all(target.parent().unwrap()).unwrap();
        std::fs::write(&target, b"an old daemon").unwrap();

        copy(&source, &target).expect("replace the daemon");

        assert_eq!(std::fs::read(&target).unwrap(), b"the new daemon");
    }

    #[test]
    fn copy_leaves_the_daemon_alone_when_it_is_the_source() {
        let dir = TempDir::new();
        let target = dir.join("unisond.exe");
        std::fs::write(&target, b"the daemon").unwrap();

        copy(&target, &target).expect("install the daemon next to itself");

        assert_eq!(std::fs::read(&target).unwrap(), b"the daemon");
    }

    #[test]
    fn copy_reports_a_directory_that_cannot_be_created() {
        let dir = TempDir::new();
        let source = dir.join("unisond.exe");
        std::fs::write(&source, b"the daemon").unwrap();
        let file = dir.join("a file");
        std::fs::write(&file, b"").unwrap();

        let err =
            copy(&source, &file.join("unisond.exe")).expect_err("a file was taken for a directory");
        assert!(err.starts_with("cannot create"), "{err}");
    }

    #[test]
    fn last_line_skips_the_empty_end_of_the_report() {
        assert_eq!(last_line("a step\n\n"), Some("a step"));
        assert_eq!(
            last_line("a step\r\nanother step\r\n"),
            Some("another step")
        );
        assert_eq!(last_line(""), None);
        assert_eq!(last_line("  \n\n"), None);
    }

    #[test]
    fn reason_from_log_drops_the_prefix_of_the_hidden_mode() {
        assert_eq!(
            reason_from_log("unison: copying\nunison: the daemon is gone\n"),
            Some("the daemon is gone".to_string())
        );
        assert_eq!(
            reason_from_log("a message\n"),
            Some("a message".to_string())
        );
        assert_eq!(reason_from_log(""), None);
    }

    #[test]
    fn offer_names_match_the_window() {
        assert_eq!(serde_json::to_string(&Offer::Off).unwrap(), r#""off""#);
        assert_eq!(
            serde_json::to_string(&Offer::Available).unwrap(),
            r#""available""#
        );
        assert_eq!(
            serde_json::to_string(&Offer::Missing).unwrap(),
            r#""missing""#
        );
    }

    #[test]
    fn the_installer_and_the_app_share_the_names() {
        assert!(HOOKS.contains(RUN_KEY), "the installer writes another key");
        assert!(
            HOOKS.contains(&format!("!define UNISON_RUN_VALUE \"{RUN_VALUE}\"")),
            "the installer writes another startup value"
        );
        assert!(
            HOOKS.contains(&format!("!define UNISON_FIREWALL_RULE \"{FIREWALL_RULE}\"")),
            "the installer uses another rule name"
        );
        assert!(
            HOOKS.contains(&format!("profile={FIREWALL_PROFILES}")),
            "the installer covers other network profiles"
        );
        assert!(
            HOOKS.contains(&format!("protocol={FIREWALL_PROTOCOL}")),
            "the installer allows another protocol"
        );
        assert!(
            HOOKS.contains(&format!(r"$INSTDIR\{BINARY}")),
            "the installer puts another daemon into the installation directory"
        );
    }

    #[cfg(not(windows))]
    #[test]
    fn the_app_installs_the_daemon_on_windows_only() {
        assert_eq!(offer(), Offer::Off);
        let err = install().expect_err("the daemon was installed on a system without one");
        assert!(err.contains("only on Windows"), "{err}");
    }
}
