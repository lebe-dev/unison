mod daemon;
mod proxy;
mod tray;

use base64::Engine;
use base64::engine::general_purpose::STANDARD as BASE64;
use serde::Serialize;
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

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_notification::init())
        .setup(|app| {
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
            tray::set_tray_state
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
