//! Tray icon of the app.
//!
//! The tray reports the state the webview sends and forwards the pause choices
//! to the webview, which owns the calls of the local API.

use serde::{Deserialize, Serialize};
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem, Submenu};
use tauri::tray::TrayIconBuilder;
use tauri::{AppHandle, Emitter, Manager, State, Wry};

/// EVENT_TRAY_ACTION is emitted to the webview when the user picks a tray item
/// that the webview has to act on.
pub const EVENT_TRAY_ACTION: &str = "tray-action";

/// TrayAction is the payload of EVENT_TRAY_ACTION.
#[derive(Clone, Serialize)]
#[serde(tag = "action", rename_all = "snake_case")]
pub enum TrayAction {
    /// The user picked a pause mode.
    Pause { mode: PauseChoice },
}

/// PauseChoice is the pause mode of the clipboard the user picked.
#[derive(Clone, Copy, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum PauseChoice {
    FiveMinutes,
    OneHour,
    Indefinite,
    Off,
}

/// TrayState is the state the webview reports for the menu.
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct TrayState {
    /// The daemon answers the local API calls.
    pub connected: bool,
    /// Number of paired devices that are online.
    pub online_peers: usize,
    /// The clipboard synchronization is paused.
    pub paused: bool,
}

/// TrayMenu keeps the items whose text follows the daemon state.
pub struct TrayMenu {
    status: MenuItem<Wry>,
    pause: MenuItem<Wry>,
}

/// install adds the tray icon with its menu.
pub fn install(app: &AppHandle) -> tauri::Result<()> {
    let status = MenuItem::with_id(app, "status", "Daemon offline", false, None::<&str>)?;
    let pause_state = MenuItem::with_id(app, "pause-state", "Clipboard on", false, None::<&str>)?;
    let five_minutes =
        MenuItem::with_id(app, "pause-5m", "Pause for 5 minutes", true, None::<&str>)?;
    let one_hour = MenuItem::with_id(app, "pause-1h", "Pause for 1 hour", true, None::<&str>)?;
    let indefinite = MenuItem::with_id(
        app,
        "pause-indefinite",
        "Pause until I resume",
        true,
        None::<&str>,
    )?;
    let off = MenuItem::with_id(app, "pause-off", "Resume", true, None::<&str>)?;
    let pause_menu = Submenu::with_items(
        app,
        "Clipboard pause",
        true,
        &[&five_minutes, &one_hour, &indefinite, &off],
    )?;
    let open = MenuItem::with_id(app, "open", "Open Unison", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "Quit Unison", true, None::<&str>)?;

    let menu = Menu::with_items(
        app,
        &[
            &status,
            &pause_state,
            &PredefinedMenuItem::separator(app)?,
            &pause_menu,
            &PredefinedMenuItem::separator(app)?,
            &open,
            &quit,
        ],
    )?;

    let mut tray = TrayIconBuilder::with_id("main")
        .menu(&menu)
        .tooltip("Unison")
        .show_menu_on_left_click(true)
        .on_menu_event(|app, event| on_menu_event(app, event.id().as_ref()));
    if let Some(icon) = app.default_window_icon() {
        tray = tray.icon(icon.clone());
    }
    tray.build(app)?;

    app.manage(TrayMenu {
        status,
        pause: pause_state,
    });
    Ok(())
}

/// set_tray_state updates the menu texts.
#[tauri::command]
pub fn set_tray_state(menu: State<'_, TrayMenu>, state: TrayState) -> Result<(), String> {
    menu.status
        .set_text(status_text(&state))
        .map_err(|err| err.to_string())?;
    menu.pause
        .set_text(pause_text(state.paused))
        .map_err(|err| err.to_string())?;
    Ok(())
}

fn on_menu_event(app: &AppHandle, id: &str) {
    match id {
        "pause-5m" => pause(app, PauseChoice::FiveMinutes),
        "pause-1h" => pause(app, PauseChoice::OneHour),
        "pause-indefinite" => pause(app, PauseChoice::Indefinite),
        "pause-off" => pause(app, PauseChoice::Off),
        "open" => show_window(app),
        "quit" => app.exit(0),
        _ => {}
    }
}

fn pause(app: &AppHandle, mode: PauseChoice) {
    if let Err(err) = app.emit(EVENT_TRAY_ACTION, TrayAction::Pause { mode }) {
        eprintln!("unison: sending the tray action failed: {err}");
    }
}

fn show_window(app: &AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
}

fn status_text(state: &TrayState) -> String {
    if !state.connected {
        return "Daemon offline".to_string();
    }
    match state.online_peers {
        0 => "No devices online".to_string(),
        1 => "1 device online".to_string(),
        peers => format!("{peers} devices online"),
    }
}

fn pause_text(paused: bool) -> &'static str {
    if paused {
        "Clipboard paused"
    } else {
        "Clipboard on"
    }
}
