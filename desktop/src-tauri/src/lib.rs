pub mod agent;
mod daemon;
mod prompt;
mod proxy;
mod tray;

use base64::Engine;
use base64::engine::general_purpose::STANDARD as BASE64;
use serde::Serialize;
use std::time::Duration;
use tauri::ipc::Channel;
use tauri::{Manager, State};

/// AppState is shared by the commands.
struct AppState {
    /// Endpoint of the local API, or the reason it is unknown.
    endpoint: Result<String, String>,
}

/// StreamChunk is one chunk of a streaming response. The bytes are base64
/// encoded so they can travel through the IPC as JSON.
#[derive(Clone, Serialize)]
struct StreamChunk {
    chunk: String,
}

/// api_call forwards a unary call of the local API to the daemon.
#[tauri::command]
async fn api_call(
    state: State<'_, AppState>,
    service: String,
    method: String,
    body: String,
) -> Result<proxy::ApiResponse, String> {
    let endpoint = state.endpoint.as_ref().map_err(|err| err.clone())?;
    let body = BASE64
        .decode(body)
        .map_err(|err| format!("the request body is not base64: {err}"))?;
    proxy::call(endpoint, &service, &method, &body).await
}

/// api_stream forwards a server streaming call of the local API to the daemon
/// and sends every chunk of the response body to the given channel.
#[tauri::command]
async fn api_stream(
    state: State<'_, AppState>,
    service: String,
    method: String,
    body: String,
    on_chunk: Channel<StreamChunk>,
) -> Result<(), String> {
    let endpoint = state.endpoint.as_ref().map_err(|err| err.clone())?;
    let body = BASE64
        .decode(body)
        .map_err(|err| format!("the request body is not base64: {err}"))?;
    proxy::stream(endpoint, &service, &method, &body, |data| {
        on_chunk
            .send(StreamChunk {
                chunk: BASE64.encode(data),
            })
            .map_err(|err| format!("delivering the response failed: {err}"))
    })
    .await
}

/// daemon_status reports the state of the daemon and the reason of the last
/// failed start.
/// LOCAL_NETWORK_PANE is the Local Network pane of the privacy settings.
#[cfg(target_os = "macos")]
const LOCAL_NETWORK_PANE: &str =
    "x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension?Privacy_LocalNetwork";

/// open_local_network_settings opens the settings where the user lets the
/// daemon reach the local network.
#[tauri::command]
fn open_local_network_settings() -> Result<(), String> {
    #[cfg(target_os = "macos")]
    {
        let status = std::process::Command::new("/usr/bin/open")
            .arg(LOCAL_NETWORK_PANE)
            .status()
            .map_err(|err| format!("cannot open the settings: {err}"))?;
        if !status.success() {
            return Err(format!("cannot open the settings: {status}"));
        }
        Ok(())
    }
    #[cfg(not(target_os = "macos"))]
    {
        Err("the local network settings exist only on macOS".to_string())
    }
}

#[tauri::command]
fn daemon_status() -> daemon::Status {
    daemon::status()
}

/// login_item_status reports the state of the login item of the daemon.
#[tauri::command]
fn login_item_status() -> agent::LoginItem {
    agent::LoginItem {
        status: agent::status(),
    }
}

/// set_login_item registers or unregisters the login item of the daemon. A
/// change keeps the daemon running: the login item starts it at login and a
/// direct start covers the rest of the session after it is turned off.
#[tauri::command]
async fn set_login_item(
    state: State<'_, AppState>,
    enabled: bool,
) -> Result<agent::LoginItem, String> {
    let endpoint = state.endpoint.as_ref().ok().cloned();
    // Only the macOS login item is a process the system owns, so only macOS has
    // to hand the running daemon over to it. The Windows entry starts the
    // daemon at the next login and leaves the running one alone.
    let managed = cfg!(target_os = "macos");
    // The daemon of the app holds the lock and the endpoint, so it steps aside
    // before the login item starts its own.
    if managed
        && enabled
        && daemon::stop_started()
        && let Some(path) = &endpoint
    {
        daemon::wait_stopped(path, Duration::from_secs(5)).await;
    }

    let status = match agent::set(enabled) {
        Ok(status) => status,
        Err(err) => {
            if let Some(path) = endpoint {
                tauri::async_runtime::spawn(async move {
                    if let Err(err) = daemon::ensure_running(&path).await {
                        eprintln!("unison: {err}");
                    }
                });
            }
            return Err(err);
        }
    };

    if managed && let Some(path) = endpoint {
        tauri::async_runtime::spawn(async move {
            if !enabled {
                daemon::wait_stopped(&path, Duration::from_secs(5)).await;
            }
            if let Err(err) = daemon::ensure_running(&path).await {
                eprintln!("unison: {err}");
            }
        });
    }
    Ok(agent::LoginItem { status })
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_dialog::init())
        .setup(|app| {
            if let Some(Err(err)) = agent::register_at_launch() {
                eprintln!("unison: the login item could not be registered: {err}");
            }
            let endpoint = daemon::resolve_endpoint();
            match &endpoint {
                Ok(path) => {
                    let path = path.clone();
                    tauri::async_runtime::spawn(async move {
                        if let Err(err) = daemon::ensure_running(&path).await {
                            eprintln!("unison: {err}");
                        }
                    });
                }
                // The window still has to tell why the app knows no daemon.
                Err(err) => daemon::set_failed(err),
            }
            app.manage(AppState { endpoint });
            tray::install(app.handle())?;
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            api_call,
            api_stream,
            daemon_status,
            open_local_network_settings,
            login_item_status,
            set_login_item,
            tray::set_tray_state,
            tray::pick_files,
            tray::pick_folder,
            prompt::show_prompt,
            prompt::hide_prompt
        ])
        .on_window_event(|window, event| {
            // The window stays alive in the tray, so the app keeps the daemon
            // events and the menu state up to date while it is hidden.
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = window.hide();
            }
        })
        .run(tauri::generate_context!())
        .expect("error while running Unison");
}
