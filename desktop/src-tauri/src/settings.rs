//! Settings document the user chooses to keep or restore.
//!
//! The commands read and write one file the user picked. They never touch a
//! file the window did not name, and the size of a document stays small.

use std::fs;
use std::path::Path;

/// MAX_BYTES bounds a settings document. The daemon refuses a larger one too.
const MAX_BYTES: u64 = 512 << 10;

/// absolute checks that the window passed a path the user picked.
fn absolute(path: &str) -> Result<&Path, String> {
    let file = Path::new(path);
    if !file.is_absolute() {
        return Err("the path is not absolute".to_string());
    }
    Ok(file)
}

/// settings_write writes the settings document to the path the user picked.
#[tauri::command]
pub fn settings_write(path: String, text: String) -> Result<(), String> {
    let file = absolute(&path)?;
    if text.len() as u64 > MAX_BYTES {
        return Err("the settings document is too large".to_string());
    }
    fs::write(file, text.as_bytes()).map_err(|err| format!("cannot write the settings file: {err}"))
}

/// settings_read returns the settings document of the path the user picked.
#[tauri::command]
pub fn settings_read(path: String) -> Result<String, String> {
    let file = absolute(&path)?;
    let meta = fs::metadata(file).map_err(|err| format!("cannot read the settings file: {err}"))?;
    if meta.len() > MAX_BYTES {
        return Err("the settings document is too large".to_string());
    }
    fs::read_to_string(file).map_err(|err| format!("cannot read the settings file: {err}"))
}

#[cfg(test)]
mod tests {
    use super::*;

    /// test_path returns a file inside the temporary directory.
    fn test_path(name: &str) -> String {
        std::env::temp_dir()
            .join(name)
            .to_string_lossy()
            .into_owned()
    }

    #[test]
    fn a_document_round_trips() {
        let path = test_path("unison-settings-test.json");
        settings_write(path.clone(), "{\"schema\":\"unison-settings\"}".to_string()).unwrap();
        let read = settings_read(path.clone()).unwrap();
        assert_eq!(read, "{\"schema\":\"unison-settings\"}");
        let _ = fs::remove_file(path);
    }

    #[test]
    fn a_relative_path_is_refused() {
        assert!(settings_write("settings.json".to_string(), "{}".to_string()).is_err());
        assert!(settings_read("settings.json".to_string()).is_err());
    }

    #[test]
    fn a_large_document_is_refused() {
        let path = test_path("unison-settings-large.json");
        let text = "x".repeat(MAX_BYTES as usize + 1);
        assert!(settings_write(path, text).is_err());
    }
}
