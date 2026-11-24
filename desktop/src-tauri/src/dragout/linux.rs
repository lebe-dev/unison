//! Native drags out of the window on Linux.

use std::path::PathBuf;

/// start_drag starts a native drag of the staged files. The copies stay after
/// the drop.
pub(super) fn start_drag(_window: &tauri::Window, _paths: Vec<PathBuf>) -> Result<(), String> {
    Err("dragging files out of the window is not supported yet".to_string())
}
