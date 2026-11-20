//! Files of a transfer that the user opens from a notification.
//!
//! The commands hand the paths to the file manager or to the apps the system
//! keeps for the file types.

use std::path::Path;
use std::process::Command;

/// show_in_folder reveals the files in the file manager of the system.
#[tauri::command]
pub async fn show_in_folder(paths: Vec<String>) -> Result<(), String> {
    reveal(&absolute_paths(paths)?).await
}

/// open_files opens the files with the apps the system keeps for them.
#[tauri::command]
pub async fn open_files(paths: Vec<String>) -> Result<(), String> {
    open(&absolute_paths(paths)?).await
}

/// absolute_paths keeps only the paths the system commands take as files,
/// since a relative path would be read as an option.
fn absolute_paths(paths: Vec<String>) -> Result<Vec<String>, String> {
    if paths.is_empty() {
        return Err("there are no files to open".to_string());
    }
    if let Some(path) = paths.iter().find(|path| !Path::new(path).is_absolute()) {
        return Err(format!("the path is not absolute: {path}"));
    }
    Ok(paths)
}

/// unexpected reports a file command the system could not run and returns the
/// message with the details the window shows.
fn unexpected(message: String) -> String {
    crate::observability::capture_error("the file command could not run");
    crate::journal::error(&format!("the file command failed: {message}"));
    message
}

/// file_uri returns the `file://` URI of an absolute path, with the characters
/// the URI syntax reserves encoded.
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
fn file_uri(path: &str) -> String {
    let mut uri = String::from("file://");
    for byte in path.bytes() {
        match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'.' | b'_' | b'~' | b'/' => {
                uri.push(byte as char);
            }
            _ => uri.push_str(&format!("%{byte:02X}")),
        }
    }
    uri
}

/// parent_dirs returns the directories of the paths, each one once.
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
fn parent_dirs(paths: &[String]) -> Vec<String> {
    let mut dirs: Vec<String> = Vec::new();
    for path in paths {
        let Some(dir) = Path::new(path).parent() else {
            continue;
        };
        let dir = dir.to_string_lossy().into_owned();
        if !dirs.contains(&dir) {
            dirs.push(dir);
        }
    }
    dirs
}

/// reveal selects the paths in the file manager.
#[cfg(target_os = "macos")]
async fn reveal(paths: &[String]) -> Result<(), String> {
    let mut args = vec!["-R"];
    args.extend(paths.iter().map(String::as_str));
    run("/usr/bin/open", &args)
}

#[cfg(target_os = "windows")]
async fn reveal(paths: &[String]) -> Result<(), String> {
    for path in paths {
        let select = format!("/select,{path}");
        run("explorer.exe", &[select.as_str()])?;
    }
    Ok(())
}

/// reveal asks the file manager of the desktop to select the files. A desktop
/// without the file manager interface opens the folder that holds them, which
/// is the closest the app can get without knowing the file manager.
#[cfg(target_os = "linux")]
async fn reveal(paths: &[String]) -> Result<(), String> {
    if file_manager::show_items(paths).await.is_ok() {
        return Ok(());
    }
    open(&parent_dirs(paths)).await
}

#[cfg(not(any(target_os = "macos", target_os = "windows", target_os = "linux")))]
async fn reveal(_paths: &[String]) -> Result<(), String> {
    Err("this system has no file manager the app can reach".to_string())
}

/// open starts every path with the app the system keeps for its type.
#[cfg(target_os = "macos")]
async fn open(paths: &[String]) -> Result<(), String> {
    let args: Vec<&str> = paths.iter().map(String::as_str).collect();
    run("/usr/bin/open", &args)
}

#[cfg(target_os = "windows")]
async fn open(paths: &[String]) -> Result<(), String> {
    for path in paths {
        run("explorer.exe", &[path.as_str()])?;
    }
    Ok(())
}

/// open hands the paths to the desktop, which starts the app of each type. The
/// path is an argument of the command, so no shell reads it.
#[cfg(target_os = "linux")]
async fn open(paths: &[String]) -> Result<(), String> {
    for path in paths {
        let status = Command::new("xdg-open")
            .arg(path)
            .stdin(std::process::Stdio::null())
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .status()
            .map_err(|err| unexpected(format!("cannot run xdg-open: {err}")))?;
        if !status.success() {
            return Err(format!("xdg-open failed: {status}"));
        }
    }
    Ok(())
}

#[cfg(not(any(target_os = "macos", target_os = "windows", target_os = "linux")))]
async fn open(_paths: &[String]) -> Result<(), String> {
    Err("this system has no apps the app can start".to_string())
}

/// run starts the program and waits for it to end.
#[cfg(target_os = "macos")]
fn run(program: &str, args: &[&str]) -> Result<(), String> {
    let status = Command::new(program)
        .args(args)
        .status()
        .map_err(|err| unexpected(format!("cannot run {program}: {err}")))?;
    if !status.success() {
        return Err(format!("{program} failed: {status}"));
    }
    Ok(())
}

/// The Explorer of Windows reports a failure even when it works, so the call
/// only checks that the shell could be started.
#[cfg(target_os = "windows")]
fn run(program: &str, args: &[&str]) -> Result<(), String> {
    Command::new(program)
        .args(args)
        .spawn()
        .map(|_| ())
        .map_err(|err| unexpected(format!("cannot run {program}: {err}")))
}

/// file_manager is the interface the desktop file managers implement.
#[cfg(target_os = "linux")]
mod file_manager {
    /// FILE_MANAGER names the service and the object of the interface.
    const SERVICE: &str = "org.freedesktop.FileManager1";

    #[zbus::proxy(
        interface = "org.freedesktop.FileManager1",
        default_service = "org.freedesktop.FileManager1",
        default_path = "/org/freedesktop/FileManager1"
    )]
    trait FileManager1 {
        /// show_items asks the file manager to show the files.
        fn show_items(&self, uris: &[&str], startup_id: &str) -> zbus::Result<()>;
    }

    /// show_items asks the file manager of the session to show the files. The
    /// call fails when no file manager answers.
    pub async fn show_items(paths: &[String]) -> Result<(), String> {
        let uris: Vec<String> = paths.iter().map(|path| super::file_uri(path)).collect();
        let uris: Vec<&str> = uris.iter().map(String::as_str).collect();
        let connection = zbus::Connection::session()
            .await
            .map_err(|err| format!("cannot reach the session bus: {err}"))?;
        let proxy = FileManager1Proxy::new(&connection)
            .await
            .map_err(|err| format!("{SERVICE} is not available: {err}"))?;
        proxy
            .show_items(&uris, "")
            .await
            .map_err(|err| format!("{SERVICE} refused the request: {err}"))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn keeps_the_absolute_paths() {
        let paths = vec!["/Users/mac/Downloads/report.pdf".to_string()];
        assert_eq!(
            absolute_paths(paths).unwrap(),
            vec!["/Users/mac/Downloads/report.pdf"]
        );
    }

    #[test]
    fn refuses_an_empty_list() {
        let err = absolute_paths(Vec::new()).expect_err("an empty list was accepted");
        assert!(err.starts_with("there are no files"), "{err}");
    }

    #[test]
    fn refuses_a_relative_path() {
        for path in ["report.pdf", "-R", ""] {
            let err =
                absolute_paths(vec![path.to_string()]).expect_err("a relative path was accepted");
            assert!(err.starts_with("the path is not absolute"), "{err}");
        }
    }

    #[test]
    fn builds_the_uris_of_the_files() {
        assert_eq!(file_uri("/home/user/a.txt"), "file:///home/user/a.txt");
        assert_eq!(
            file_uri("/home/user/Документы/a b.txt"),
            "file:///home/user/%D0%94%D0%BE%D0%BA%D1%83%D0%BC%D0%B5%D0%BD%D1%82%D1%8B/a%20b.txt"
        );
        assert_eq!(file_uri("/a#b?c"), "file:///a%23b%3Fc");
    }

    #[test]
    fn parents_of_the_files_come_once() {
        let paths = vec![
            "/home/user/Downloads/a.txt".to_string(),
            "/home/user/Downloads/b.txt".to_string(),
            "/home/user/Pictures/c.png".to_string(),
        ];
        assert_eq!(
            parent_dirs(&paths),
            vec!["/home/user/Downloads", "/home/user/Pictures"]
        );
        assert_eq!(parent_dirs(&["/a".to_string()]), vec!["/"]);
    }
}
