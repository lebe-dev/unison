//! Native drags out of the window on Linux.

use std::path::PathBuf;

use crate::dragout::preview;

/// start_drag starts a native drag of the staged files. The copies stay after
/// the drop, and the call returns once the drag is scheduled on the main thread.
pub(super) fn start_drag(window: &tauri::Window, paths: Vec<PathBuf>) -> Result<(), String> {
    let image = preview(&paths);
    let options = drag::Options {
        mode: drag::DragMode::Copy,
        ..drag::Options::default()
    };
    let drag_window = window.clone();
    window
        .run_on_main_thread(move || {
            // The crate sets the drag source on the window and begins the session
            // with the pointer button that is still pressed.
            let gtk_window = match drag_window.gtk_window() {
                Ok(gtk_window) => gtk_window,
                Err(err) => {
                    crate::journal::error(&format!("cannot reach the GTK window: {err}"));
                    return;
                }
            };
            let item = drag::DragItem::Files(paths);
            if let Err(err) = drag::start_drag(&gtk_window, item, image, |_, _| {}, options) {
                crate::journal::error(&format!("the drag did not start: {err}"));
            }
        })
        .map_err(|err| format!("cannot reach the main thread: {err}"))
}
