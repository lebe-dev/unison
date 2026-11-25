//! Native drags out of the window on macOS.

use std::path::PathBuf;

use crate::dragout::preview;

/// start_drag starts a native drag of the staged files. The copies stay after
/// the drop, and the call returns once the session has started.
pub(super) fn start_drag(window: &tauri::Window, paths: Vec<PathBuf>) -> Result<(), String> {
    let image = preview(&paths);
    let options = drag::Options {
        mode: drag::DragMode::Copy,
        ..drag::Options::default()
    };
    let drag_window = window.clone();
    window
        .run_on_main_thread(move || {
            let item = drag::DragItem::Files(paths);
            if let Err(err) = drag::start_drag(&drag_window, item, image, |_, _| {}, options) {
                crate::journal::error(&format!("the drag did not start: {err}"));
            }
        })
        .map_err(|err| format!("cannot reach the main thread: {err}"))
}
