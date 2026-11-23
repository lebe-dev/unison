//! Files another app shares with Unison.
//!
//! The share extension of the bundle hands the shared files to the app as an
//! open request. The paths wait in the inbox until the webview takes them, so a
//! request that starts the app is not lost while the page still loads.

use std::path::{Path, PathBuf};
use std::sync::Mutex;
use tauri::{AppHandle, Emitter, Manager, State, Url};

/// EVENT_SHARE_FILES is emitted to the webview when shared files wait in the
/// inbox.
pub const EVENT_SHARE_FILES: &str = "share-files";

/// Inbox keeps the shared paths until the webview takes them.
#[derive(Default)]
pub struct Inbox {
    paths: Mutex<Vec<String>>,
}

impl Inbox {
    /// push adds paths after the ones that still wait, leaving out the paths
    /// that already wait.
    pub fn push(&self, paths: Vec<String>) {
        let mut waiting = self.paths.lock().unwrap_or_else(|err| err.into_inner());
        for path in paths {
            if !waiting.contains(&path) {
                waiting.push(path);
            }
        }
    }

    /// take returns the waiting paths and empties the inbox.
    pub fn take(&self) -> Vec<String> {
        let mut waiting = self.paths.lock().unwrap_or_else(|err| err.into_inner());
        std::mem::take(&mut *waiting)
    }
}

/// file_paths returns the paths of the regular files among the URLs. Other
/// schemes and directories are left out, since only files go into an offer.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
pub fn file_paths(urls: &[Url], is_file: impl Fn(&Path) -> bool) -> Vec<String> {
    urls.iter()
        .filter(|url| url.scheme() == "file")
        .filter_map(|url| url.to_file_path().ok())
        .filter(|path: &PathBuf| path.is_absolute() && is_file(path))
        .filter_map(|path| path.to_str().map(str::to_string))
        .collect()
}

/// receive puts the shared files into the inbox, tells the webview and brings
/// the window to the front.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
pub fn receive(app: &AppHandle, urls: &[Url]) {
    let paths = file_paths(urls, Path::is_file);
    if paths.is_empty() {
        crate::journal::error("the shared items contain no files to send");
        return;
    }
    if let Some(inbox) = app.try_state::<Inbox>() {
        inbox.push(paths);
    }
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
    if let Err(err) = app.emit(EVENT_SHARE_FILES, ()) {
        eprintln!("unison: telling the window about the shared files failed: {err}");
    }
}

/// take_shared_files returns the shared paths that wait for an offer.
#[tauri::command]
pub fn take_shared_files(inbox: State<'_, Inbox>) -> Vec<String> {
    inbox.take()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn url(text: &str) -> Url {
        Url::parse(text).unwrap()
    }

    #[test]
    fn file_paths_keeps_files() {
        let urls = [
            url("file:///Users/me/report.pdf"),
            url("file:///Users/me/My%20Photo.jpg"),
        ];
        assert_eq!(
            file_paths(&urls, |_| true),
            vec!["/Users/me/report.pdf", "/Users/me/My Photo.jpg"]
        );
    }

    #[test]
    fn file_paths_leaves_out_other_schemes() {
        let urls = [
            url("https://example.com/a.pdf"),
            url("unison://send?path=/etc/passwd"),
            url("file:///tmp/a.bin"),
        ];
        assert_eq!(file_paths(&urls, |_| true), vec!["/tmp/a.bin"]);
    }

    #[test]
    fn file_paths_leaves_out_directories() {
        let urls = [
            url("file:///Users/me/Folder/"),
            url("file:///Users/me/a.txt"),
        ];
        let paths = file_paths(&urls, |path| path.extension().is_some());
        assert_eq!(paths, vec!["/Users/me/a.txt"]);
    }

    #[test]
    fn inbox_returns_paths_once() {
        let inbox = Inbox::default();
        inbox.push(vec!["/a".into(), "/b".into()]);
        inbox.push(vec!["/b".into(), "/c".into()]);
        assert_eq!(inbox.take(), vec!["/a", "/b", "/c"]);
        assert!(inbox.take().is_empty());
    }
}
