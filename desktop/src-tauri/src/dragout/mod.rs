//! Dragging the content of a clip out of the window as files, and copying it
//! to the system clipboard.
//!
//! The shell copies an item into a staging directory of the caches and starts a
//! native drag of the copies. The copies stay on disk after the drop, so a
//! terminal inserts a path that still points at the content. The native part of
//! the drag and the clipboard write belongs to the backend of the platform.

use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{Duration, SystemTime};
use tauri::Manager;

#[cfg(target_os = "linux")]
mod linux;
#[cfg(target_os = "macos")]
mod macos;
#[cfg(target_os = "windows")]
mod windows;

#[cfg(target_os = "linux")]
use linux as platform;
#[cfg(target_os = "macos")]
use macos as platform;
#[cfg(target_os = "windows")]
use windows as platform;

/// How long the copies of a dropped item stay on disk.
pub const STAGING_RETENTION: Duration = Duration::from_secs(24 * 60 * 60);

/// Name of the directory that holds the staging directories, inside the caches.
const STAGING_DIR: &str = "drag";

/// Icon of a drag that carries no image.
const APP_ICON: &[u8] = include_bytes!("../../icons/128x128.png");

/// Extensions the system shows as an image, lowercase.
const IMAGE_EXTENSIONS: [&str; 9] = [
    "png", "jpg", "jpeg", "gif", "webp", "heic", "heif", "tif", "tiff",
];

/// staging_root returns the directory that holds the staging directories.
fn staging_root(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    app.path()
        .app_cache_dir()
        .map(|dir| dir.join(STAGING_DIR))
        .map_err(|err| format!("cannot find the cache directory: {err}"))
}

/// valid_id reports whether an item id fits in a directory name.
fn valid_id(id: &str) -> bool {
    !id.is_empty()
        && id.len() <= 64
        && id
            .chars()
            .all(|char| char.is_ascii_alphanumeric() || char == '-' || char == '_')
}

/// unique returns a suffix that keeps two staging directories apart.
fn unique() -> String {
    static NEXT: AtomicU64 = AtomicU64::new(0);
    let nanos = SystemTime::now()
        .duration_since(SystemTime::UNIX_EPOCH)
        .map(|elapsed| elapsed.as_nanos())
        .unwrap_or_default();
    format!("{nanos:x}-{:x}", NEXT.fetch_add(1, Ordering::Relaxed))
}

/// restrict makes a directory reachable only by the user.
#[cfg(unix)]
fn restrict(path: &Path) -> std::io::Result<()> {
    use std::os::unix::fs::PermissionsExt;
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o700))
}

#[cfg(not(unix))]
fn restrict(_path: &Path) -> std::io::Result<()> {
    Ok(())
}

/// drag_staging_dir creates an empty directory that receives the copies of one
/// item.
#[tauri::command]
pub fn drag_staging_dir(app: tauri::AppHandle, item_id: String) -> Result<String, String> {
    if !valid_id(&item_id) {
        return Err("the item id has unexpected characters".to_string());
    }
    let root = staging_root(&app)?;
    std::fs::create_dir_all(&root)
        .map_err(|err| format!("cannot create the staging directory: {err}"))?;
    restrict(&root).map_err(|err| format!("cannot restrict the staging directory: {err}"))?;
    let dir = root.join(format!("{item_id}-{}", unique()));
    std::fs::create_dir(&dir)
        .map_err(|err| format!("cannot create the staging directory: {err}"))?;
    restrict(&dir).map_err(|err| format!("cannot restrict the staging directory: {err}"))?;
    Ok(dir.to_string_lossy().into_owned())
}

/// staged_paths returns the canonical paths below the staging root and refuses
/// every other path.
fn staged_paths(root: &Path, paths: &[String]) -> Result<Vec<PathBuf>, String> {
    if paths.is_empty() {
        return Err("there are no files to drag".to_string());
    }
    let mut staged = Vec::with_capacity(paths.len());
    for path in paths {
        let canonical = Path::new(path)
            .canonicalize()
            .map_err(|err| format!("cannot read the staged file: {err}"))?;
        if !canonical.starts_with(root) {
            return Err("the file is not in the staging directory".to_string());
        }
        staged.push(canonical);
    }
    Ok(staged)
}

/// is_image reports whether the path looks like an image the system can show.
fn is_image(path: &Path) -> bool {
    path.extension()
        .and_then(|extension| extension.to_str())
        .is_some_and(|extension| {
            IMAGE_EXTENSIONS
                .iter()
                .any(|known| extension.eq_ignore_ascii_case(known))
        })
}

/// preview returns the image that follows the cursor: the first staged image,
/// or the icon of the app.
fn preview(paths: &[PathBuf]) -> drag::Image {
    match paths.iter().find(|path| is_image(path)) {
        Some(path) => drag::Image::File(path.clone()),
        None => drag::Image::Raw(APP_ICON.to_vec()),
    }
}

/// start_file_drag starts a native drag of the staged files. The copies stay
/// after the drop.
#[tauri::command]
pub fn start_file_drag(
    window: tauri::Window,
    app: tauri::AppHandle,
    paths: Vec<String>,
) -> Result<(), String> {
    let root = staging_root(&app)?
        .canonicalize()
        .map_err(|err| format!("cannot read the staging directory: {err}"))?;
    let staged = staged_paths(&root, &paths)?;
    platform::start_drag(&window, staged)
}

/// purge_staging removes the copies that are older than the retention.
pub fn purge_staging(app: &tauri::AppHandle) {
    let Ok(root) = staging_root(app) else {
        return;
    };
    if let Err(err) = purge_older_than(&root, SystemTime::now(), STAGING_RETENTION) {
        crate::journal::record(
            "WARN",
            &format!("the staged drag files were not removed: {err}"),
        );
    }
}

/// purge_older_than removes the entries of the directory that were not changed
/// for the given time. A missing directory is not an error.
fn purge_older_than(root: &Path, now: SystemTime, retention: Duration) -> std::io::Result<()> {
    let entries = match std::fs::read_dir(root) {
        Ok(entries) => entries,
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => return Ok(()),
        Err(err) => return Err(err),
    };
    for entry in entries.flatten() {
        let path = entry.path();
        let Ok(modified) = entry.metadata().and_then(|metadata| metadata.modified()) else {
            continue;
        };
        if now.duration_since(modified).unwrap_or_default() <= retention {
            continue;
        }
        if path.is_dir() {
            let _ = std::fs::remove_dir_all(&path);
        } else {
            let _ = std::fs::remove_file(&path);
        }
    }
    Ok(())
}

#[cfg(all(test, unix))]
mod tests {
    use super::*;
    use crate::testutil::TempDir;

    #[test]
    fn accepts_the_ids_of_the_daemon() {
        assert!(valid_id("0f8fad5b-d9cb-469f-a165-70867728950e"));
        assert!(valid_id("abc_1"));
    }

    #[test]
    fn refuses_ids_that_could_leave_the_directory() {
        for id in ["", "../x", "a/b", "a b", &"a".repeat(65)] {
            assert!(!valid_id(id), "{id}");
        }
    }

    #[test]
    fn keeps_only_the_files_below_the_staging_root() {
        let dir = TempDir::new();
        std::fs::create_dir_all(dir.join("drag/item")).expect("create the staging directory");
        let root = dir.join("drag").canonicalize().expect("canonicalize");
        let staged_file = dir.join("drag/item/clip.png");
        std::fs::write(&staged_file, b"content").expect("write the file");
        let outside = dir.join("secret.txt");
        std::fs::write(&outside, b"content").expect("write the file");

        let staged = staged_paths(&root, &[staged_file.to_string_lossy().into_owned()])
            .expect("the staged file was refused");
        assert_eq!(staged.len(), 1);

        let err = staged_paths(&root, &[outside.to_string_lossy().into_owned()])
            .expect_err("a file outside of the root was accepted");
        assert!(
            err.starts_with("the file is not in the staging directory"),
            "{err}"
        );
    }

    #[test]
    fn refuses_an_empty_list_of_files() {
        let dir = TempDir::new();
        let err = staged_paths(dir.path(), &[]).expect_err("an empty list was accepted");
        assert!(err.starts_with("there are no files"), "{err}");
    }

    #[test]
    fn removes_the_staging_directories_older_than_a_day() {
        let dir = TempDir::new();
        let root = dir.join("drag");
        std::fs::create_dir_all(root.join("item")).expect("create the staging directory");

        purge_older_than(
            &root,
            SystemTime::now() + Duration::from_secs(25 * 60 * 60),
            STAGING_RETENTION,
        )
        .expect("purge the directory");

        assert!(!root.join("item").exists());
    }

    #[test]
    fn keeps_the_recent_staging_directories() {
        let dir = TempDir::new();
        let root = dir.join("drag");
        std::fs::create_dir_all(root.join("item")).expect("create the staging directory");

        purge_older_than(&root, SystemTime::now(), STAGING_RETENTION).expect("purge the directory");

        assert!(root.join("item").exists());
    }

    #[test]
    fn misses_a_staging_root_that_was_never_created() {
        let dir = TempDir::new();
        purge_older_than(&dir.join("drag"), SystemTime::now(), STAGING_RETENTION)
            .expect("a missing directory is not an error");
    }

    #[test]
    fn follows_the_images_of_a_drag() {
        assert!(is_image(Path::new("/caches/drag/item/clip.png")));
        assert!(is_image(Path::new("/caches/drag/item/photo.JPG")));
        assert!(!is_image(Path::new("/caches/drag/item/report.pdf")));
        assert!(!is_image(Path::new("/caches/drag/item/no-extension")));
    }
}
