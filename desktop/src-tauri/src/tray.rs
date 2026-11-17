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
#[derive(Clone, Copy, Debug, PartialEq, Serialize)]
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

/// pick_folder opens the native folder dialog and returns the picked path.
#[tauri::command]
pub async fn pick_folder(app: AppHandle) -> Result<Option<String>, String> {
    let picked =
        tauri::async_runtime::spawn_blocking(move || app.dialog().file().blocking_pick_folder())
            .await
            .map_err(|err| err.to_string())?;
    Ok(picked.and_then(path_string))
}

/// path_string turns a picked file path into plain text.
fn path_string(path: tauri_plugin_dialog::FilePath) -> Option<String> {
    path.into_path()
        .ok()
        .map(|path| path.to_string_lossy().into_owned())
}

/// MenuCommand is what a picked menu item asks for.
#[derive(Debug, PartialEq)]
enum MenuCommand<'a> {
    Pause(PauseChoice),
    CopyItem(&'a str),
    SendFiles(&'a str),
    Open,
    Quit,
}

/// menu_command returns the command of a menu item id. Items that only show a
/// state, and dynamic items without a target, ask for nothing.
fn menu_command(id: &str) -> Option<MenuCommand<'_>> {
    if let Some(item) = id.strip_prefix(HISTORY_PREFIX) {
        return (!item.is_empty()).then_some(MenuCommand::CopyItem(item));
    }
    if let Some(device) = id.strip_prefix(SEND_PREFIX) {
        return (!device.is_empty()).then_some(MenuCommand::SendFiles(device));
    }
    let command = match id {
        "pause-5m" => MenuCommand::Pause(PauseChoice::FiveMinutes),
        "pause-1h" => MenuCommand::Pause(PauseChoice::OneHour),
        "pause-indefinite" => MenuCommand::Pause(PauseChoice::Indefinite),
        "pause-off" => MenuCommand::Pause(PauseChoice::Off),
        "open" => MenuCommand::Open,
        "quit" => MenuCommand::Quit,
        _ => return None,
    };
    Some(command)
}

fn on_menu_event(app: &AppHandle, id: &str) {
    let Some(command) = menu_command(id) else {
        return;
    };
    match command {
        MenuCommand::Pause(mode) => emit(app, TrayAction::Pause { mode }),
        MenuCommand::CopyItem(id) => emit(app, TrayAction::CopyItem { id: id.to_string() }),
        MenuCommand::SendFiles(device) => send_files(app, device),
        MenuCommand::Open => show_window(app),
        MenuCommand::Quit => app.exit(0),
    }
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
    let next = entries(
        HISTORY_PREFIX,
        items
            .iter()
            .map(|item| (item.id.as_str(), item.label.as_str())),
    );
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
    let next = entries(
        SEND_PREFIX,
        devices
            .iter()
            .map(|device| (device.id.as_str(), device.name.as_str())),
    );
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

/// entries returns the menu ids and labels of a dynamic list. An entry without
/// an id has no target, so it is left out.
fn entries<'a>(
    prefix: &str,
    items: impl Iterator<Item = (&'a str, &'a str)>,
) -> Vec<(String, String)> {
    items
        .filter(|(id, _)| !id.is_empty())
        .map(|(id, label)| (format!("{prefix}{id}"), menu_label(label)))
        .collect()
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

#[cfg(test)]
mod tests {
    use super::*;

    fn state(connected: bool, online_peers: usize) -> TrayState {
        TrayState {
            connected,
            online_peers,
            paused: false,
            devices: Vec::new(),
            recent: Vec::new(),
        }
    }

    #[test]
    fn menu_label_keeps_short_text() {
        assert_eq!(menu_label("hello"), "hello");
    }

    #[test]
    fn menu_label_shortens_long_text_by_characters() {
        let text = "ж".repeat(MENU_LABEL_LIMIT + 5);
        let label = menu_label(&text);
        assert_eq!(label, format!("{}...", "ж".repeat(MENU_LABEL_LIMIT)));
    }

    #[test]
    fn menu_label_puts_text_on_one_line() {
        assert_eq!(menu_label("a\nb\r\nc"), "a b  c");
    }

    #[test]
    fn status_text_counts_devices() {
        assert_eq!(status_text(&state(false, 3)), "Daemon offline");
        assert_eq!(status_text(&state(true, 0)), "No devices online");
        assert_eq!(status_text(&state(true, 1)), "1 device online");
        assert_eq!(status_text(&state(true, 4)), "4 devices online");
    }

    #[test]
    fn pause_text_names_the_state() {
        assert_eq!(pause_text(true), "Clipboard paused");
        assert_eq!(pause_text(false), "Clipboard on");
    }

    #[test]
    fn menu_command_routes_the_fixed_items() {
        assert_eq!(
            menu_command("pause-5m"),
            Some(MenuCommand::Pause(PauseChoice::FiveMinutes))
        );
        assert_eq!(
            menu_command("pause-1h"),
            Some(MenuCommand::Pause(PauseChoice::OneHour))
        );
        assert_eq!(
            menu_command("pause-indefinite"),
            Some(MenuCommand::Pause(PauseChoice::Indefinite))
        );
        assert_eq!(
            menu_command("pause-off"),
            Some(MenuCommand::Pause(PauseChoice::Off))
        );
        assert_eq!(menu_command("open"), Some(MenuCommand::Open));
        assert_eq!(menu_command("quit"), Some(MenuCommand::Quit));
    }

    #[test]
    fn menu_command_routes_the_dynamic_items() {
        assert_eq!(
            menu_command("history:01HX"),
            Some(MenuCommand::CopyItem("01HX"))
        );
        assert_eq!(
            menu_command("send:dev:1"),
            Some(MenuCommand::SendFiles("dev:1")),
            "only the prefix is cut from the id"
        );
    }

    #[test]
    fn menu_command_ignores_items_without_a_command() {
        for id in [
            "status",
            "pause-state",
            "recent-empty",
            "send-empty",
            "history:",
            "send:",
            "",
            "QUIT",
        ] {
            assert_eq!(menu_command(id), None, "{id:?} asked for a command");
        }
    }

    #[test]
    fn entries_build_the_ids_of_the_menu() {
        let long = "x".repeat(MENU_LABEL_LIMIT + 1);
        let built = entries(
            SEND_PREFIX,
            [("a", "Laptop"), ("", "No id"), ("b", long.as_str())].into_iter(),
        );
        assert_eq!(
            built,
            vec![
                ("send:a".to_string(), "Laptop".to_string()),
                (
                    "send:b".to_string(),
                    format!("{}...", "x".repeat(MENU_LABEL_LIMIT))
                ),
            ]
        );
        for (id, _) in &built {
            assert!(matches!(menu_command(id), Some(MenuCommand::SendFiles(_))));
        }
        let recent = entries(HISTORY_PREFIX, [("7", "text")].into_iter());
        assert_eq!(menu_command(&recent[0].0), Some(MenuCommand::CopyItem("7")));
    }

    #[test]
    fn tray_state_reads_the_state_of_the_webview() {
        let state: TrayState = serde_json::from_str(
            r#"{
                "connected": true,
                "onlinePeers": 2,
                "paused": true,
                "devices": [{"id": "d1", "name": "Laptop"}],
                "recent": [{"id": "h1", "label": "copied text"}]
            }"#,
        )
        .expect("parse the state");
        assert!(state.connected && state.paused);
        assert_eq!(state.online_peers, 2);
        assert_eq!(state.devices[0].id, "d1");
        assert_eq!(state.devices[0].name, "Laptop");
        assert_eq!(state.recent[0].id, "h1");
        assert_eq!(state.recent[0].label, "copied text");
    }

    #[test]
    fn tray_state_lists_are_optional() {
        let state: TrayState =
            serde_json::from_str(r#"{"connected": false, "onlinePeers": 0, "paused": false}"#)
                .expect("parse the state");
        assert!(state.devices.is_empty() && state.recent.is_empty());
    }

    #[test]
    fn tray_actions_serialize_for_the_webview() {
        let json = |action: TrayAction| serde_json::to_string(&action).unwrap();
        assert_eq!(
            json(TrayAction::Pause {
                mode: PauseChoice::FiveMinutes
            }),
            r#"{"action":"pause","mode":"five_minutes"}"#
        );
        assert_eq!(
            json(TrayAction::Pause {
                mode: PauseChoice::OneHour
            }),
            r#"{"action":"pause","mode":"one_hour"}"#
        );
        assert_eq!(
            json(TrayAction::Pause {
                mode: PauseChoice::Indefinite
            }),
            r#"{"action":"pause","mode":"indefinite"}"#
        );
        assert_eq!(
            json(TrayAction::Pause {
                mode: PauseChoice::Off
            }),
            r#"{"action":"pause","mode":"off"}"#
        );
        assert_eq!(
            json(TrayAction::CopyItem {
                id: "h1".to_string()
            }),
            r#"{"action":"copy_item","id":"h1"}"#
        );
        assert_eq!(
            json(TrayAction::SendFiles {
                device_id: "d1".to_string(),
                paths: vec!["/a b.txt".to_string()],
            }),
            r#"{"action":"send_files","deviceId":"d1","paths":["/a b.txt"]}"#
        );
    }

    #[test]
    fn replace_entries_reports_changes_only() {
        let current = Mutex::new(Vec::new());
        let next = vec![("1".to_string(), "one".to_string())];
        assert!(replace_entries(&current, &next));
        assert!(
            !replace_entries(&current, &next),
            "an unchanged list rebuilt the menu"
        );
        assert!(replace_entries(&current, &[]));
        assert!(current.lock().unwrap().is_empty());
    }
}
