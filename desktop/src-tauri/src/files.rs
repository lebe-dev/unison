//! Files of a transfer that the user opens from a notification.
//!
//! The commands hand the paths to the file manager or to the apps the system
//! keeps for the file types.

use std::path::Path;
use std::process::Command;

/// show_in_folder reveals the files in the file manager of the system.
#[tauri::command]
pub fn show_in_folder(paths: Vec<String>) -> Result<(), String> {
    reveal(&absolute_paths(paths)?)
}

/// open_files opens the files with the apps the system keeps for them.
#[tauri::command]
pub fn open_files(paths: Vec<String>) -> Result<(), String> {
    open(&absolute_paths(paths)?)
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

/// reveal selects the paths in the file manager.
#[cfg(target_os = "macos")]
fn reveal(paths: &[String]) -> Result<(), String> {
    let mut args = vec!["-R"];
    args.extend(paths.iter().map(String::as_str));
    run("/usr/bin/open", &args)
}

#[cfg(target_os = "windows")]
fn reveal(paths: &[String]) -> Result<(), String> {
    for path in paths {
        let select = format!("/select,{path}");
        run("explorer.exe", &[select.as_str()])?;
    }
    Ok(())
}

#[cfg(not(any(target_os = "macos", target_os = "windows")))]
fn reveal(_paths: &[String]) -> Result<(), String> {
    Err("this system has no file manager the app can reach".to_string())
}

/// open starts every path with the app the system keeps for its type.
#[cfg(target_os = "macos")]
fn open(paths: &[String]) -> Result<(), String> {
    let args: Vec<&str> = paths.iter().map(String::as_str).collect();
    run("/usr/bin/open", &args)
}

#[cfg(target_os = "windows")]
fn open(paths: &[String]) -> Result<(), String> {
    for path in paths {
        run("explorer.exe", &[path.as_str()])?;
    }
    Ok(())
}

#[cfg(not(any(target_os = "macos", target_os = "windows")))]
fn open(_paths: &[String]) -> Result<(), String> {
    Err("this system has no apps the app can start".to_string())
}

/// run starts the program and waits for it to end.
#[cfg(target_os = "macos")]
fn run(program: &str, args: &[&str]) -> Result<(), String> {
    let status = Command::new(program)
        .args(args)
        .status()
        .map_err(|err| format!("cannot run {program}: {err}"))?;
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
        .map_err(|err| format!("cannot run {program}: {err}"))
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
}
