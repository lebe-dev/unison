//! The prompt window asks the questions that wait for the user: a clipboard
//! item a device holds for confirmation and a file offer. It is a small window
//! above the others, so a question is seen while the main window is hidden in
//! the tray.

use tauri::{
    AppHandle, Manager, PhysicalPosition, WebviewUrl, WebviewWindow, WebviewWindowBuilder,
};

/// PROMPT_LABEL is the label of the prompt window. The capability of the app
/// lists it, so the window may use the local API.
const PROMPT_LABEL: &str = "prompt";

/// Logical size of the prompt window. The height leaves room for the details
/// of an introduction.
const WIDTH: f64 = 360.0;
const HEIGHT: f64 = 210.0;

/// MARGIN keeps the window off the edges of the screen, in logical pixels.
const MARGIN: f64 = 16.0;

/// show_prompt brings the prompt window up, creating it on first use. The
/// command is async on purpose: a synchronous command runs on the main thread,
/// and creating a webview there deadlocks on Windows, which freezes every later
/// call of the app.
#[tauri::command]
pub async fn show_prompt(app: AppHandle) -> Result<(), String> {
    let window = match app.get_webview_window(PROMPT_LABEL) {
        Some(window) => window,
        None => create(&app).map_err(|err| format!("cannot open the prompt window: {err}"))?,
    };
    place(&window);
    window
        .show()
        .map_err(|err| format!("cannot show the prompt window: {err}"))?;
    let _ = window.unminimize();
    let _ = window.set_focus();
    Ok(())
}

/// hide_prompt puts the prompt window away. The questions stay with the app.
/// It is async for the same reason as show_prompt.
#[tauri::command]
pub async fn hide_prompt(app: AppHandle) -> Result<(), String> {
    let Some(window) = app.get_webview_window(PROMPT_LABEL) else {
        return Ok(());
    };
    window
        .hide()
        .map_err(|err| format!("cannot hide the prompt window: {err}"))
}

/// create builds the prompt window, hidden until it is placed.
fn create(app: &AppHandle) -> tauri::Result<WebviewWindow> {
    WebviewWindowBuilder::new(app, PROMPT_LABEL, WebviewUrl::App("prompt.html".into()))
        .title("Unison")
        .inner_size(WIDTH, HEIGHT)
        .resizable(false)
        .minimizable(false)
        .maximizable(false)
        .always_on_top(true)
        .visible_on_all_workspaces(true)
        .skip_taskbar(true)
        .visible(false)
        .build()
}

/// place moves the window to the top right corner of the screen with the
/// cursor, below the menu bar, where the system shows its notifications.
fn place(window: &WebviewWindow) {
    let monitor = match window.current_monitor() {
        Ok(Some(monitor)) => monitor,
        _ => match window.primary_monitor() {
            Ok(Some(monitor)) => monitor,
            _ => return,
        },
    };
    let area = monitor.work_area();
    let position = corner(area.position, area.size.width, monitor.scale_factor());
    if let Err(err) = window.set_position(position) {
        eprintln!("unison: placing the prompt window failed: {err}");
    }
}

/// corner returns the position of the window in the top right corner of the
/// work area that starts at origin and is width physical pixels wide.
fn corner(origin: PhysicalPosition<i32>, width: u32, scale: f64) -> PhysicalPosition<i32> {
    let window = (WIDTH * scale) as i32;
    let margin = (MARGIN * scale) as i32;
    let x = origin.x + width as i32 - window - margin;
    let y = origin.y + margin;
    PhysicalPosition::new(x, y)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn corner_keeps_the_margin_from_the_top_right_edges() {
        let position = corner(PhysicalPosition::new(0, 25), 1440, 1.0);
        assert_eq!(position, PhysicalPosition::new(1440 - 360 - 16, 25 + 16));
    }

    #[test]
    fn corner_scales_to_physical_pixels() {
        let position = corner(PhysicalPosition::new(0, 50), 2880, 2.0);
        assert_eq!(position, PhysicalPosition::new(2880 - 720 - 32, 50 + 32));
    }

    #[test]
    fn corner_follows_a_monitor_left_of_the_main_one() {
        let position = corner(PhysicalPosition::new(-1920, 0), 1920, 1.0);
        assert_eq!(position, PhysicalPosition::new(-360 - 16, 16));
        assert!(position.x < 0, "the window left its monitor");
    }
}
