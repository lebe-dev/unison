//! The application log of the shell and the export of the log files.
//!
//! The shell appends its own records to `logs/app.log` while the daemon keeps
//! the marker `logs/journal.on`, so the toggle in the settings applies without
//! a restart. The records use the format of the journal of the core: a UTC
//! timestamp with milliseconds, the level, the message and the attributes.
//! Absolute paths of the user are replaced with a placeholder.
//!
//! The export is written to a temporary file and offered by the share sheet of
//! macOS. When the share sheet cannot be shown, the window falls back to a save
//! dialog and reveals the chosen file in Finder.

use std::fs::{self, OpenOptions};
use std::io::{ErrorKind, Write};
use std::path::{Path, PathBuf};
#[cfg(target_os = "macos")]
use std::process::Command;
use std::sync::Mutex;
use std::time::{SystemTime, UNIX_EPOCH};

use serde::Serialize;
use tauri::AppHandle;

use crate::redact;

/// Names and limits of the log files.
const DIR_NAME: &str = "logs";
const APP_FILE: &str = "app.log";
const JOURNAL_FILE: &str = "journal.log";
const MARKER_NAME: &str = "journal.on";
const APP_MAX_SIZE: u64 = 2 << 20;
const APP_BACKUPS: usize = 2;
const JOURNAL_BACKUPS: usize = 3;

/// WRITE serializes the appends and the rotations of the shell log.
static WRITE: Mutex<()> = Mutex::new(());

/// ShellInfo describes the build and the system, which the export header shows.
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ShellInfo {
    pub app_version: String,
    pub os: String,
}

/// SavedLog is the export file the shell wrote and may share.
#[derive(Serialize)]
pub struct SavedLog {
    pub name: String,
    pub path: String,
}

/// record writes one record when the user keeps the application log on.
pub fn record(level: &str, message: &str) {
    if !matches!(level, "DEBUG" | "INFO" | "WARN" | "ERROR") {
        return;
    }
    let Ok(dir) = crate::daemon::data_dir() else {
        return;
    };
    if !marker_exists(&dir) {
        return;
    }
    let dir = dir.join(DIR_NAME);
    // A panic can interrupt a record that holds the lock, so a busy lock drops
    // this record instead of waiting for a thread that will not finish.
    let Ok(_guard) = WRITE.try_lock() else {
        return;
    };
    let record = line(SystemTime::now(), level, message);
    let _ = append(
        &dir.join(APP_FILE),
        record.as_bytes(),
        APP_MAX_SIZE,
        APP_BACKUPS,
    );
}

/// marker_exists reports whether the user keeps the journal on.
fn marker_exists(data_dir: &Path) -> bool {
    data_dir.join(DIR_NAME).join(MARKER_NAME).exists()
}

/// error writes an error record of the shell.
pub fn error(message: &str) {
    record("ERROR", message);
}

/// install_panic_hook records the panics of the shell before the previous hook
/// handles them.
pub fn install_panic_hook() {
    let previous = std::panic::take_hook();
    std::panic::set_hook(Box::new(move |info| {
        let payload = info
            .payload()
            .downcast_ref::<&str>()
            .map(|text| (*text).to_string())
            .or_else(|| info.payload().downcast_ref::<String>().cloned())
            .unwrap_or_else(|| "the shell panicked".to_string());
        let place = info
            .location()
            .map(|location| format!(" at {}:{}", location.file(), location.line()))
            .unwrap_or_default();
        error(&format!("panic{place}: {payload}"));
        previous(info);
    }));
}

/// line returns the line of a record, always in UTC.
fn line(at: SystemTime, level: &str, message: &str) -> String {
    format!("{} {} {}\n", timestamp(at), level, text(message))
}

/// text keeps a record on one line and removes the paths of the user.
fn text(message: &str) -> String {
    let clean: String = message
        .chars()
        .map(|c| if c < ' ' || c == '\x7f' { ' ' } else { c })
        .collect();
    redact::paths(&clean)
}

/// timestamp formats a time as the journal of the core does.
fn timestamp(at: SystemTime) -> String {
    let millis = at
        .duration_since(UNIX_EPOCH)
        .map(|elapsed| elapsed.as_millis() as i64)
        .unwrap_or(0);
    let (date, time) = parts(millis);
    format!(
        "{:04}-{:02}-{:02}T{:02}:{:02}:{:02}.{:03}Z",
        date.0,
        date.1,
        date.2,
        time.0,
        time.1,
        time.2,
        millis.rem_euclid(1000)
    )
}

/// file_stamp formats a time for the name of the export file.
fn file_stamp(at: SystemTime) -> String {
    let millis = at
        .duration_since(UNIX_EPOCH)
        .map(|elapsed| elapsed.as_millis() as i64)
        .unwrap_or(0);
    let (date, time) = parts(millis);
    format!(
        "{:04}{:02}{:02}-{:02}{:02}{:02}",
        date.0, date.1, date.2, time.0, time.1, time.2
    )
}

/// parts returns the UTC date and time of milliseconds since the epoch.
fn parts(millis: i64) -> ((i64, u32, u32), (u32, u32, u32)) {
    let seconds = millis.div_euclid(1000);
    let days = seconds.div_euclid(86_400);
    let rest = seconds.rem_euclid(86_400);
    (
        civil_from_days(days),
        (
            (rest / 3600) as u32,
            ((rest % 3600) / 60) as u32,
            (rest % 60) as u32,
        ),
    )
}

/// civil_from_days returns the year, month and day of days since the epoch.
fn civil_from_days(days: i64) -> (i64, u32, u32) {
    let z = days + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = z - era * 146_097;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let year = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let month = (5 * doy + 2) / 153;
    let day = doy - (153 * month + 2) / 5 + 1;
    let month = if month < 10 { month + 3 } else { month - 9 };
    let year = if month <= 2 { year + 1 } else { year };
    (year, month as u32, day as u32)
}

/// append writes a line to a log file and rotates it when it grows past the
/// limit.
fn append(path: &Path, data: &[u8], limit: u64, keep: usize) -> Result<(), String> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)
            .map_err(|err| format!("cannot create the log directory: {err}"))?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let _ = fs::set_permissions(parent, fs::Permissions::from_mode(0o700));
        }
    }
    if let Ok(metadata) = fs::metadata(path)
        && metadata.len() > 0
        && metadata.len() + data.len() as u64 > limit
    {
        rotate(path, keep)?;
    }
    let mut file = OpenOptions::new()
        .create(true)
        .append(true)
        .open(path)
        .map_err(|err| format!("cannot open the log file: {err}"))?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = file.set_permissions(fs::Permissions::from_mode(0o600));
    }
    file.write_all(data)
        .map_err(|err| format!("cannot write the log file: {err}"))
}

/// rotate moves the log files by one suffix and drops the oldest file.
fn rotate(path: &Path, keep: usize) -> Result<(), String> {
    let suffixed =
        |index: usize| -> PathBuf { PathBuf::from(format!("{}.{index}", path.to_string_lossy())) };
    let _ = fs::remove_file(suffixed(keep));
    for index in (1..keep).rev() {
        let from = suffixed(index);
        if from.exists() {
            fs::rename(&from, suffixed(index + 1))
                .map_err(|err| format!("cannot rotate the log file: {err}"))?;
        }
    }
    if path.exists() {
        fs::rename(path, suffixed(1))
            .map_err(|err| format!("cannot rotate the log file: {err}"))?;
    }
    Ok(())
}

/// read_lines returns the current records of the shell and the core, merged by
/// time.
fn read_lines() -> Result<Vec<String>, String> {
    read_lines_in(&crate::daemon::data_dir()?.join(DIR_NAME), false)
}

/// read_all_lines returns the records of both logs with their backups.
fn read_all_lines() -> Result<Vec<String>, String> {
    read_lines_in(&crate::daemon::data_dir()?.join(DIR_NAME), true)
}

/// read_lines_in returns the records of the log files in a directory. The
/// backups carry the older records and are read for an export.
fn read_lines_in(dir: &Path, backups: bool) -> Result<Vec<String>, String> {
    if !dir.exists() {
        return Ok(Vec::new());
    }
    let last = if backups { JOURNAL_BACKUPS } else { 0 };
    let mut records: Vec<(String, String)> = Vec::new();
    for name in [APP_FILE, JOURNAL_FILE] {
        for index in 0..=last {
            let path = if index == 0 {
                dir.join(name)
            } else {
                dir.join(format!("{name}.{index}"))
            };
            match fs::read_to_string(&path) {
                Ok(text) => {
                    for line in text.lines() {
                        if !line.trim().is_empty() {
                            records.push((stamp_of(line).to_string(), line.to_string()));
                        }
                    }
                }
                Err(err) if err.kind() == ErrorKind::NotFound => continue,
                Err(err) => {
                    return Err(format!("cannot read the log file: {err}"));
                }
            }
        }
    }
    records.sort_by(|left, right| left.0.cmp(&right.0));
    Ok(records.into_iter().map(|(_, line)| line).collect())
}

/// stamp_of returns the timestamp token of a record.
fn stamp_of(line: &str) -> &str {
    line.split_whitespace().next().unwrap_or("")
}

/// os_name returns the name and the version of the system.
fn os_name() -> String {
    #[cfg(target_os = "macos")]
    {
        let version = Command::new("/usr/bin/sw_vers")
            .arg("-productVersion")
            .output()
            .ok()
            .filter(|output| output.status.success())
            .map(|output| String::from_utf8_lossy(&output.stdout).trim().to_string())
            .filter(|version| !version.is_empty());
        os_label(version.as_deref())
    }
    #[cfg(not(target_os = "macos"))]
    {
        std::env::consts::OS.to_string()
    }
}

/// os_label formats the product version of macOS.
#[cfg(target_os = "macos")]
fn os_label(version: Option<&str>) -> String {
    match version {
        Some(version) => format!("macOS {version}"),
        None => "macOS".to_string(),
    }
}

/// journal_shell_info reports the version of the app and of the system.
#[tauri::command]
pub fn journal_shell_info() -> ShellInfo {
    ShellInfo {
        app_version: env!("UNISON_VERSION").to_string(),
        os: os_name(),
    }
}

/// journal_enabled reports whether the marker turns the journal on, which is
/// the only local answer while the daemon does not respond.
#[tauri::command]
pub fn journal_enabled() -> bool {
    crate::daemon::data_dir().is_ok_and(|dir| marker_exists(&dir))
}

/// journal_read returns the current records of both logs, newest last.
#[tauri::command]
pub fn journal_read() -> Result<Vec<String>, String> {
    read_lines()
}

/// journal_read_all returns the records of both logs with their backups.
#[tauri::command]
pub fn journal_read_all() -> Result<Vec<String>, String> {
    read_all_lines()
}

/// journal_record writes a record of the window, such as a failed copy.
#[tauri::command]
pub fn journal_record(level: String, message: String) {
    record(&level, &message);
}

/// journal_write writes the export to the temporary directory.
#[tauri::command]
pub fn journal_write(text: String) -> Result<SavedLog, String> {
    let name = format!("unison-log-{}.log", file_stamp(SystemTime::now()));
    let path = std::env::temp_dir().join(&name);
    fs::write(&path, text.as_bytes()).map_err(|err| format!("cannot write the log file: {err}"))?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = fs::set_permissions(&path, fs::Permissions::from_mode(0o600));
    }
    Ok(SavedLog {
        name,
        path: path.to_string_lossy().into_owned(),
    })
}

/// journal_save copies the export to the path the user picked.
#[tauri::command]
pub fn journal_save(source: String, target: String) -> Result<(), String> {
    if !Path::new(&target).is_absolute() {
        return Err("the path is not absolute".to_string());
    }
    let source =
        fs::canonicalize(&source).map_err(|err| format!("cannot read the log file: {err}"))?;
    let temp = fs::canonicalize(std::env::temp_dir())
        .map_err(|err| format!("cannot read the temporary directory: {err}"))?;
    if !source.starts_with(&temp) {
        return Err("the log file is not in the temporary directory".to_string());
    }
    fs::copy(&source, &target)
        .map(|_| ())
        .map_err(|err| format!("cannot save the log file: {err}"))
}

/// journal_share opens the share sheet of macOS with the export file.
#[tauri::command]
pub async fn journal_share(app: AppHandle, path: String) -> Result<(), String> {
    #[cfg(target_os = "macos")]
    {
        let (sender, receiver) = tokio::sync::oneshot::channel();
        let handle = app.clone();
        app.run_on_main_thread(move || {
            let _ = sender.send(platform::share(&handle, &path));
        })
        .map_err(|err| format!("cannot reach the main thread: {err}"))?;
        receiver
            .await
            .map_err(|_| "the share sheet did not start".to_string())?
    }
    #[cfg(not(target_os = "macos"))]
    {
        let _ = (app, path);
        Err("the share sheet is available only on macOS".to_string())
    }
}

/// platform shows the share sheet anchored to the main window.
#[cfg(target_os = "macos")]
mod platform {
    use std::cell::RefCell;

    use objc2::rc::Retained;
    use objc2::runtime::AnyClass;
    use objc2::{AnyThread, MainThreadMarker};
    use objc2_app_kit::{NSSharingServicePicker, NSWindow};
    use objc2_foundation::{NSArray, NSPoint, NSRect, NSRectEdge, NSSize, NSString, NSURL};
    use tauri::Manager;

    thread_local! {
        /// PICKER keeps the open share sheet alive until the next one.
        static PICKER: RefCell<Option<Retained<NSSharingServicePicker>>> = const { RefCell::new(None) };
    }

    /// share offers the file in the share sheet of the main window.
    pub fn share(app: &tauri::AppHandle, path: &str) -> Result<(), String> {
        MainThreadMarker::new()
            .ok_or_else(|| "the share sheet needs the main thread".to_string())?;
        let window = app
            .get_webview_window("main")
            .ok_or_else(|| "the main window is not available".to_string())?;
        let handle = window
            .ns_window()
            .map_err(|err| format!("the main window has no native handle: {err}"))?;
        // SAFETY: the handle is the NSWindow of the main window, which lives as
        // long as the app.
        let window: &NSWindow = unsafe { &*handle.cast::<NSWindow>() };
        let view = window
            .contentView()
            .ok_or_else(|| "the main window has no content view".to_string())?;
        let url = NSURL::fileURLWithPath(&NSString::from_str(path));
        let array =
            AnyClass::get(c"NSArray").ok_or_else(|| "the system has no arrays".to_string())?;
        // SAFETY: the array class takes any object and returns an array.
        let items: Retained<NSArray> = unsafe { objc2::msg_send![array, arrayWithObject: &*url] };
        // SAFETY: the picker takes file URLs, which are items it can share.
        let picker = unsafe {
            NSSharingServicePicker::initWithItems(NSSharingServicePicker::alloc(), &items)
        };
        let bounds = view.bounds();
        let rect = NSRect::new(
            NSPoint::new(bounds.size.width / 2.0, bounds.size.height - 24.0),
            NSSize::new(1.0, 1.0),
        );
        picker.showRelativeToRect_ofView_preferredEdge(rect, &view, NSRectEdge::NSMinYEdge);
        PICKER.with(|slot| *slot.borrow_mut() = Some(picker));
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// fixed returns the time of the example record.
    fn fixed() -> SystemTime {
        UNIX_EPOCH + std::time::Duration::from_millis(1_794_944_882_123)
    }

    #[test]
    fn formats_the_line_of_a_record() {
        assert_eq!(
            line(fixed(), "DEBUG", "connected to the peer"),
            "2026-11-17T19:48:02.123Z DEBUG connected to the peer\n"
        );
    }

    #[test]
    fn keeps_a_record_on_one_line() {
        assert_eq!(
            line(fixed(), "WARN", "two\nlines"),
            "2026-11-17T19:48:02.123Z WARN two lines\n"
        );
    }

    #[test]
    fn removes_the_paths_of_the_user() {
        assert_eq!(
            line(
                fixed(),
                "ERROR",
                "cannot open /Users/someone/Library/Unison"
            ),
            "2026-11-17T19:48:02.123Z ERROR cannot open [path]\n"
        );
    }

    #[test]
    fn formats_the_name_of_the_export() {
        assert_eq!(file_stamp(fixed()), "20261117-194802");
    }

    #[test]
    fn shell_info_serializes_for_the_webview() {
        let info = ShellInfo {
            app_version: "1.4.0".to_string(),
            os: "macOS 14.5".to_string(),
        };
        assert_eq!(
            serde_json::to_string(&info).unwrap(),
            r#"{"appVersion":"1.4.0","os":"macOS 14.5"}"#
        );
        let saved = SavedLog {
            name: "unison-log-1.log".to_string(),
            path: "/tmp/unison-log-1.log".to_string(),
        };
        assert_eq!(
            serde_json::to_string(&saved).unwrap(),
            r#"{"name":"unison-log-1.log","path":"/tmp/unison-log-1.log"}"#
        );
    }

    #[cfg(target_os = "macos")]
    #[test]
    fn labels_the_system() {
        assert_eq!(os_label(Some("14.5")), "macOS 14.5");
        assert_eq!(os_label(None), "macOS");
    }

    #[test]
    fn reads_the_stamp_of_a_record() {
        assert_eq!(
            stamp_of("2026-11-17T19:48:02.123Z INFO a fact"),
            "2026-11-17T19:48:02.123Z"
        );
        assert_eq!(stamp_of("a broken line"), "a");
    }

    #[cfg(unix)]
    #[test]
    fn reads_the_marker_of_the_journal() {
        let dir = crate::testutil::TempDir::new();
        assert!(!marker_exists(dir.path()));
        fs::create_dir_all(dir.join(DIR_NAME)).expect("create the log directory");
        fs::write(dir.join(DIR_NAME).join(MARKER_NAME), "on\n").expect("write the marker");
        assert!(marker_exists(dir.path()));
    }

    #[cfg(unix)]
    #[test]
    fn merges_the_records_of_both_logs() {
        let dir = crate::testutil::TempDir::new();
        fs::write(dir.join(APP_FILE), "2026-11-17T19:48:03.000Z INFO third\n")
            .expect("write the app log");
        fs::write(
            dir.join(JOURNAL_FILE),
            "2026-11-17T19:48:01.000Z WARN first\n2026-11-17T19:48:02.000Z DEBUG second\n",
        )
        .expect("write the journal");

        let lines = read_lines_in(dir.path(), true).expect("read the logs");
        assert_eq!(
            lines,
            [
                "2026-11-17T19:48:01.000Z WARN first",
                "2026-11-17T19:48:02.000Z DEBUG second",
                "2026-11-17T19:48:03.000Z INFO third",
            ]
        );
    }

    #[cfg(unix)]
    #[test]
    fn reads_only_the_current_files_for_the_window() {
        let dir = crate::testutil::TempDir::new();
        fs::write(
            dir.join(APP_FILE),
            "2026-11-17T19:48:03.000Z INFO current\n",
        )
        .expect("write the app log");
        fs::write(
            dir.join("app.log.1"),
            "2026-11-17T19:48:01.000Z INFO backup\n",
        )
        .expect("write the backup");

        let current = read_lines_in(dir.path(), false).expect("read the current logs");
        let all = read_lines_in(dir.path(), true).expect("read every log");
        assert_eq!(current, ["2026-11-17T19:48:03.000Z INFO current"]);
        assert_eq!(
            all,
            [
                "2026-11-17T19:48:01.000Z INFO backup",
                "2026-11-17T19:48:03.000Z INFO current",
            ]
        );
    }

    #[cfg(unix)]
    #[test]
    fn rotates_the_log_file_and_keeps_the_limit() {
        let dir = crate::testutil::TempDir::new();
        let path = dir.join(APP_FILE);
        for word in ["first", "second", "third", "fourth"] {
            append(&path, format!("{word}\n").as_bytes(), 12, 2).expect("append");
        }
        assert!(path.exists());
        assert_eq!(fs::metadata(&path).unwrap().len(), 7);
        assert!(path.with_file_name("app.log.1").exists());
        assert!(path.with_file_name("app.log.2").exists());
        assert!(!path.with_file_name("app.log.3").exists());
        let text = fs::read_to_string(&path).unwrap();
        assert!(text.contains("fourth"), "{text}");
    }
}
