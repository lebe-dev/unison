pub mod agent;
mod daemon;
mod files;
mod i18n;
mod journal;
pub mod observability;
mod prompt;
mod proxy;
mod redact;
mod share;
mod streams;
#[cfg(all(test, unix))]
mod testutil;
mod tray;

use base64::Engine;
use base64::engine::general_purpose::STANDARD as BASE64;
use serde::Serialize;
use std::time::Duration;
use tauri::ipc::Channel;
use tauri::webview::PageLoadEvent;
use tauri::{Manager, State, Webview};

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

/// decode_body returns the bytes of a request body the webview sent as base64.
fn decode_body(body: &str) -> Result<Vec<u8>, String> {
    BASE64
        .decode(body)
        .map_err(|err| format!("the request body is not base64: {err}"))
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
    let body = decode_body(&body)?;
    proxy::call(endpoint, &service, &method, &body).await
}

/// api_stream forwards a server streaming call of the local API to the daemon
/// and sends every chunk of the response body to the given channel. The call
/// ends when the daemon ends the response or when the webview cancels it by its
/// id with api_stream_cancel.
#[tauri::command]
#[allow(clippy::too_many_arguments)]
async fn api_stream(
    state: State<'_, AppState>,
    streams: State<'_, streams::Streams>,
    webview: Webview,
    id: u32,
    service: String,
    method: String,
    body: String,
    on_chunk: Channel<StreamChunk>,
) -> Result<(), String> {
    let endpoint = state.endpoint.as_ref().map_err(|err| err.clone())?;
    let body = decode_body(&body)?;
    let stream = streams.open(webview.label(), id);
    stream
        .run(proxy::stream(endpoint, &service, &method, &body, |data| {
            on_chunk
                .send(StreamChunk {
                    chunk: BASE64.encode(data),
                })
                .map_err(|err| format!("delivering the response failed: {err}"))
        }))
        .await
}

/// api_stream_cancel ends a streaming call the webview no longer reads, which
/// closes its connection to the daemon.
#[tauri::command]
fn api_stream_cancel(streams: State<'_, streams::Streams>, webview: Webview, id: u32) {
    streams.cancel(webview.label(), id);
}

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

/// daemon_status reports the state of the daemon and the reason of the last
/// failed start.
#[tauri::command]
fn daemon_status() -> daemon::Status {
    daemon::status()
}

/// set_sentry_enabled grants or withdraws the consent of the shell to report
/// errors.
#[tauri::command]
fn set_sentry_enabled(enabled: bool) {
    observability::set_enabled(enabled);
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
            observability::capture_error("the login item could not be changed");
            if let Some(path) = endpoint {
                tauri::async_runtime::spawn(async move {
                    if let Err(err) = daemon::ensure_running(&path).await {
                        journal::error(&format!("the daemon did not start: {err}"));
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
    journal::install_panic_hook();
    tauri::Builder::default()
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_clipboard_manager::init())
        .plugin(tauri_plugin_dialog::init())
        // The inbox exists before the first event, since a share request can
        // start the app.
        .manage(share::Inbox::default())
        .setup(|app| {
            if let Some(Err(err)) = agent::register_at_launch() {
                eprintln!("unison: the login item could not be registered: {err}");
                observability::capture_error("the login item could not be registered");
            }
            let endpoint = daemon::resolve_endpoint();
            match &endpoint {
                Ok(path) => {
                    let path = path.clone();
                    tauri::async_runtime::spawn(async move {
                        if let Err(err) = daemon::ensure_running(&path).await {
                            journal::error(&format!("the daemon did not start: {err}"));
                            eprintln!("unison: {err}");
                        }
                    });
                }
                // The window still has to tell why the app knows no daemon.
                Err(err) => {
                    observability::capture_error("the endpoint of the daemon is unknown");
                    daemon::set_failed(err);
                }
            }
            app.manage(AppState { endpoint });
            app.manage(streams::Streams::default());
            tray::install(app.handle())?;
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            api_call,
            api_stream,
            api_stream_cancel,
            daemon_status,
            open_local_network_settings,
            files::show_in_folder,
            files::open_files,
            journal::journal_enabled,
            journal::journal_read,
            journal::journal_read_all,
            journal::journal_record,
            journal::journal_save,
            journal::journal_share,
            journal::journal_shell_info,
            journal::journal_write,
            login_item_status,
            set_login_item,
            set_sentry_enabled,
            share::take_shared_files,
            tray::set_tray_state,
            tray::pick_files,
            tray::pick_folder,
            prompt::show_prompt,
            prompt::hide_prompt
        ])
        .on_page_load(|webview, payload| {
            // A page that loads again has lost the streams of the previous
            // page, so their connections to the daemon are closed here.
            if payload.event() == PageLoadEvent::Started
                && let Some(streams) = webview.try_state::<streams::Streams>()
            {
                streams.cancel_owner(webview.label());
            }
        })
        .on_window_event(|window, event| {
            // The window stays alive in the tray, so the app keeps the daemon
            // events and the menu state up to date while it is hidden.
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = window.hide();
            }
        })
        .build(tauri::generate_context!())
        .expect("error while building Unison")
        .run(|app, event| {
            // The share extension opens the shared files with the app.
            #[cfg(target_os = "macos")]
            if let tauri::RunEvent::Opened { urls } = &event {
                share::receive(app, urls);
            }
            let _ = (app, event);
        });
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn decode_body_reads_base64() {
        assert_eq!(decode_body("CAE=").unwrap(), b"\x08\x01");
        assert_eq!(decode_body("").unwrap(), b"");
    }

    #[test]
    fn decode_body_refuses_other_text() {
        for body in ["not base64!", "CAE", "CA E="] {
            let err = decode_body(body).expect_err("a broken body was accepted");
            assert!(err.starts_with("the request body is not base64"), "{err}");
        }
    }

    #[test]
    fn stream_chunk_serializes_for_the_webview() {
        let chunk = StreamChunk {
            chunk: BASE64.encode(b"\x00\x00\x00\x00\x01e"),
        };
        assert_eq!(
            serde_json::to_string(&chunk).unwrap(),
            r#"{"chunk":"AAAAAAFl"}"#
        );
    }
}
