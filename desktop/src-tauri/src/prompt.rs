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

/// Logical size of the prompt window.
const WIDTH: f64 = 360.0;
const HEIGHT: f64 = 150.0;

/// MARGIN keeps the window off the edges of the screen, in logical pixels.
const MARGIN: f64 = 16.0;

/// show_prompt brings the prompt window up, creating it on first use.
#[tauri::command]
pub fn show_prompt(app: AppHandle) -> Result<(), String> {
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
#[tauri::command]
pub fn hide_prompt(app: AppHandle) -> Result<(), String> {
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
    let scale = monitor.scale_factor();
    let width = (WIDTH * scale) as i32;
    let margin = (MARGIN * scale) as i32;
    let x = area.position.x + area.size.width as i32 - width - margin;
    let y = area.position.y + margin;
    if let Err(err) = window.set_position(PhysicalPosition::new(x, y)) {
        eprintln!("unison: placing the prompt window failed: {err}");
    }
}
