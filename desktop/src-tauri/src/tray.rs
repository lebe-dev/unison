//! Tray icon of the app.
//!
//! The tray reports the state the webview sends, forwards the actions the
//! webview has to perform and picks the files of a send offer.

use std::sync::Mutex;

use serde::{Deserialize, Serialize};
use tauri::image::Image;
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem, Submenu};
use tauri::tray::TrayIconBuilder;
use tauri::{AppHandle, Emitter, Manager, State, Wry};
use tauri_plugin_dialog::DialogExt;

/// EVENT_TRAY_ACTION is emitted to the webview when the user picks a tray item
/// that the webview has to act on.
pub const EVENT_TRAY_ACTION: &str = "tray-action";

/// Prefix of the menu ids of the recent history entries.
const HISTORY_PREFIX: &str = "history:";

/// Prefix of the menu ids of the send targets.
const SEND_PREFIX: &str = "send:";

/// Longest text of a menu item of the dynamic lists.
const MENU_LABEL_LIMIT: usize = 48;

/// TrayAction is the payload of EVENT_TRAY_ACTION.
#[derive(Clone, Serialize)]
#[serde(tag = "action", rename_all = "snake_case")]
pub enum TrayAction {
    /// The user picked a pause mode.
    Pause { mode: PauseChoice },
    /// The user picked a history entry to copy back to the clipboard.
    CopyItem { id: String },
    /// The user picked files for one device.
    #[serde(rename_all = "camelCase")]
    SendFiles {
        device_id: String,
        paths: Vec<String>,
    },
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
    /// Devices that accept a file offer.
    #[serde(default)]
    pub devices: Vec<TrayDevice>,
    /// Most recent history entries, newest first.
    #[serde(default)]
    pub recent: Vec<TrayItem>,
}

/// TrayDevice is one target of the send menu.
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct TrayDevice {
    pub id: String,
    pub name: String,
}

/// TrayItem is one entry of the recent items menu.
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct TrayItem {
    pub id: String,
    pub label: String,
}

/// TrayMenu keeps the items whose text follows the daemon state.
pub struct TrayMenu {
    status: MenuItem<Wry>,
    pause: MenuItem<Wry>,
    recent: Submenu<Wry>,
    send: Submenu<Wry>,
    /// Entries the recent menu shows, to keep an unchanged state from
    /// rebuilding it.
    recent_entries: Mutex<Vec<(String, String)>>,
    /// Entries the send menu shows.
    send_entries: Mutex<Vec<(String, String)>>,
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

    let recent = Submenu::new(app, "Recent items", true)?;
    recent.append(&MenuItem::with_id(
        app,
        "recent-empty",
        "No recent items",
        false,
        None::<&str>,
    )?)?;

    let send = Submenu::new(app, "Send file to", true)?;
    send.append(&MenuItem::with_id(
        app,
        "send-empty",
        "No devices online",
        false,
        None::<&str>,
    )?)?;

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
            &recent,
            &send,
            &PredefinedMenuItem::separator(app)?,
            &open,
            &quit,
        ],
    )?;

    let mut tray = TrayIconBuilder::with_id("main")
        .menu(&menu)
        .tooltip("Unison")
        .show_menu_on_left_click(true)
        .icon_as_template(true)
        .on_menu_event(|app, event| on_menu_event(app, event.id().as_ref()));
    if let Some(icon) = tray_icon(app) {
        tray = tray.icon(icon);
    }
    tray.build(app)?;

    app.manage(TrayMenu {
        status,
        pause: pause_state,
        recent,
        send,
        recent_entries: Mutex::new(Vec::new()),
        send_entries: Mutex::new(Vec::new()),
    });
    Ok(())
}

/// tray_icon returns the image of the menu bar entry.
///
/// The glyph is a template image: black shapes on a transparent background,
/// which the menu bar tints with the theme colour. The tray scales the image to
/// a height of 18 points, so the 2x asset keeps the glyph sharp on Retina
/// displays.
#[cfg(target_os = "macos")]
fn tray_icon(_app: &AppHandle) -> Option<Image<'static>> {
    match Image::from_bytes(include_bytes!("../icons/trayTemplate@2x.png")) {
        Ok(icon) => Some(icon),
        Err(err) => {
            eprintln!("unison: loading the tray icon failed: {err}");
            None
        }
    }
}

/// tray_icon returns the image of the menu bar entry.
#[cfg(not(target_os = "macos"))]
fn tray_icon(app: &AppHandle) -> Option<Image<'static>> {
    app.default_window_icon().cloned()
}

/// set_tray_state updates the texts and the dynamic lists of the menu.
#[tauri::command]
pub fn set_tray_state(
    app: AppHandle,
    menu: State<'_, TrayMenu>,
    state: TrayState,
) -> Result<(), String> {
    menu.status
        .set_text(status_text(&state))
        .map_err(|err| err.to_string())?;
    menu.pause
        .set_text(pause_text(state.paused))
        .map_err(|err| err.to_string())?;
    fill_recent(&app, &menu, &state.recent).map_err(|err| err.to_string())?;
    fill_send(&app, &menu, &state.devices).map_err(|err| err.to_string())
}

/// pick_files opens the native file dialog and returns the picked paths.
#[tauri::command]
pub async fn pick_files(app: AppHandle) -> Result<Vec<String>, String> {
    let picked =
        tauri::async_runtime::spawn_blocking(move || app.dialog().file().blocking_pick_files())
            .await
            .map_err(|err| err.to_string())?;
    Ok(picked
        .unwrap_or_default()
        .into_iter()
        .filter_map(path_string)
        .collect())
}

/// path_string turns a picked file path into plain text.
fn path_string(path: tauri_plugin_dialog::FilePath) -> Option<String> {
    path.into_path()
        .ok()
        .map(|path| path.to_string_lossy().into_owned())
}

fn on_menu_event(app: &AppHandle, id: &str) {
    if let Some(item) = id.strip_prefix(HISTORY_PREFIX) {
        copy_item(app, item);
        return;
    }
    if let Some(device) = id.strip_prefix(SEND_PREFIX) {
        send_files(app, device);
        return;
    }
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
    emit(app, TrayAction::Pause { mode });
}

fn copy_item(app: &AppHandle, id: &str) {
    emit(app, TrayAction::CopyItem { id: id.to_string() });
}

/// send_files asks for the files of an offer and passes them to the webview.
fn send_files(app: &AppHandle, device: &str) {
    let app = app.clone();
    let target = device.to_string();
    app.clone()
        .dialog()
        .file()
        .set_title("Send files")
        .pick_files(move |paths| {
            let files: Vec<String> = paths
                .unwrap_or_default()
                .into_iter()
                .filter_map(path_string)
                .collect();
            if files.is_empty() {
                return;
            }
            emit(
                &app,
                TrayAction::SendFiles {
                    device_id: target.clone(),
                    paths: files,
                },
            );
        });
}

fn emit(app: &AppHandle, action: TrayAction) {
    if let Err(err) = app.emit(EVENT_TRAY_ACTION, action) {
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

/// fill_recent replaces the entries of the recent items menu.
fn fill_recent(app: &AppHandle, menu: &TrayMenu, items: &[TrayItem]) -> tauri::Result<()> {
    let next: Vec<(String, String)> = items
        .iter()
        .map(|item| {
            (
                format!("{HISTORY_PREFIX}{}", item.id),
                menu_label(&item.label),
            )
        })
        .collect();
    if !replace_entries(&menu.recent_entries, &next) {
        return Ok(());
    }

    clear(&menu.recent)?;
    if next.is_empty() {
        menu.recent.append(&MenuItem::with_id(
            app,
            "recent-empty",
            "No recent items",
            false,
            None::<&str>,
        )?)?;
        return Ok(());
    }
    for (id, label) in &next {
        menu.recent
            .append(&MenuItem::with_id(app, id, label, true, None::<&str>)?)?;
    }
    Ok(())
}

/// fill_send replaces the targets of the send menu.
fn fill_send(app: &AppHandle, menu: &TrayMenu, devices: &[TrayDevice]) -> tauri::Result<()> {
    let next: Vec<(String, String)> = devices
        .iter()
        .map(|device| {
            (
                format!("{SEND_PREFIX}{}", device.id),
                menu_label(&device.name),
            )
        })
        .collect();
    if !replace_entries(&menu.send_entries, &next) {
        return Ok(());
    }

    clear(&menu.send)?;
    if next.is_empty() {
        menu.send.append(&MenuItem::with_id(
            app,
            "send-empty",
            "No devices online",
            false,
            None::<&str>,
        )?)?;
        return Ok(());
    }
    for (id, label) in &next {
        menu.send
            .append(&MenuItem::with_id(app, id, label, true, None::<&str>)?)?;
    }
    Ok(())
}

/// replace_entries stores the entries of a list. It reports whether they
/// differ from the entries the menu already shows.
fn replace_entries(current: &Mutex<Vec<(String, String)>>, next: &[(String, String)]) -> bool {
    let mut entries = current.lock().unwrap_or_else(|error| error.into_inner());
    if entries.as_slice() == next {
        return false;
    }
    entries.clear();
    entries.extend_from_slice(next);
    true
}

/// clear removes every item of a submenu.
fn clear(menu: &Submenu<Wry>) -> tauri::Result<()> {
    for item in menu.items()? {
        menu.remove(&item)?;
    }
    Ok(())
}

/// menu_label shortens a text for a menu item.
fn menu_label(text: &str) -> String {
    let mut label: String = text.chars().take(MENU_LABEL_LIMIT).collect();
    if text.chars().count() > MENU_LABEL_LIMIT {
        label.push_str("...");
    }
    label.replace(['\n', '\r'], " ")
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
