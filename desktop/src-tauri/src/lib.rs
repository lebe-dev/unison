pub mod agent;
mod daemon;
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
    // The daemon of the app holds the lock and the endpoint, so it steps aside
    // before the login item starts its own.
    if enabled
        && cfg!(target_os = "macos")
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

    if let Some(path) = endpoint {
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
            if let Ok(path) = &endpoint {
                let path = path.clone();
                tauri::async_runtime::spawn(async move {
                    if let Err(err) = daemon::ensure_running(&path).await {
                        eprintln!("unison: {err}");
                    }
                });
            }
            app.manage(AppState { endpoint });
            tray::install(app.handle())?;
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            api_call,
            api_stream,
            login_item_status,
            set_login_item,
            tray::set_tray_state,
            tray::pick_files
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
