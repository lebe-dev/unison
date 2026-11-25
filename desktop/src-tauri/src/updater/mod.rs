//! Automatic updates of the desktop app.
//!
//! The updater checks the releases of GitHub, verifies the signature and the
//! hash of an artifact, and hands the verified file to the installer of the
//! platform. Everything that speaks to the network stays in this shell; the
//! window only reads a snapshot and calls the commands.

pub mod config;
pub mod github;
pub mod scheduler;
pub mod verify;
pub mod version;

#[cfg(test)]
mod testserver;

#[cfg(target_os = "linux")]
pub mod linux;
#[cfg(target_os = "macos")]
pub mod macos;
#[cfg(any(target_os = "windows", test))]
pub mod windows;

#[cfg(target_os = "linux")]
use linux as platform;
#[cfg(target_os = "macos")]
use macos as platform;
#[cfg(target_os = "windows")]
use windows as platform;

use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, MutexGuard};
use std::time::Duration;

use serde::Serialize;
use tauri::{AppHandle, Emitter, State};

use config::{CheckInterval, UpdaterConfig};
use github::ReleaseSummary;
use version::Version;

/// Event that carries the snapshot of the updater to the window.
pub const EVENT: &str = "updater-state";

/// Phase of the installation of an update.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum InstallPhase {
    Preparing,
    StoppingDaemon,
    ApplyingUpdate,
    RestartingServices,
    Relaunching,
}

/// State of the updater.
#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum UpdateStatus {
    Idle,
    Checking,
    UpToDate,
    Available,
    Downloading { downloaded: u64, total: u64 },
    Verifying,
    Installing { phase: InstallPhase },
    Restarting,
    Error { code: String, message: String },
}

impl UpdateStatus {
    /// busy reports whether the updater holds a check, a download or an
    /// installation.
    pub fn busy(&self) -> bool {
        matches!(
            self,
            Self::Checking
                | Self::Downloading { .. }
                | Self::Verifying
                | Self::Installing { .. }
                | Self::Restarting
        )
    }

    /// downloading reports whether the status belongs to a download the user
    /// may cancel.
    fn downloading(&self) -> bool {
        matches!(self, Self::Downloading { .. } | Self::Verifying)
    }
}

/// UpdaterSnapshot is what the window reads about the updater.
#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct UpdaterSnapshot {
    pub current_version: String,
    pub supported: bool,
    pub unsupported_reason: Option<String>,
    pub config: UpdaterConfig,
    pub status: UpdateStatus,
    pub available: Option<ReleaseSummary>,
    pub dismissed: bool,
}

/// Inner is the state behind the mutex.
struct Inner {
    config: UpdaterConfig,
    status: UpdateStatus,
    available: Option<github::ReleaseInfo>,
    cancel: Option<Arc<AtomicBool>>,
    /// Time of the next check after a rate limit, in unix milliseconds.
    retry_at_unix_ms: Option<i64>,
}

/// Updater owns the settings, the state and the HTTP client of the updater.
pub struct Updater {
    inner: Mutex<Inner>,
    config_changed: tokio::sync::Notify,
    http: reqwest::Client,
    data_dir: Option<PathBuf>,
}

impl Updater {
    /// new reads the settings of the data directory.
    pub fn new() -> Self {
        let data_dir = match crate::daemon::data_dir() {
            Ok(dir) => Some(dir),
            Err(err) => {
                crate::journal::record(
                    "WARN",
                    &format!("the updater has no data directory: {err}"),
                );
                None
            }
        };
        let config = data_dir
            .as_deref()
            .map(UpdaterConfig::load)
            .unwrap_or_default();
        let http = github::client().unwrap_or_else(|err| {
            crate::journal::error(&err);
            reqwest::Client::new()
        });
        Self {
            inner: Mutex::new(Inner {
                config,
                status: UpdateStatus::Idle,
                available: None,
                cancel: None,
                retry_at_unix_ms: None,
            }),
            config_changed: tokio::sync::Notify::new(),
            http,
            data_dir,
        }
    }

    /// lock takes the state, even after a panic of another thread.
    fn lock(&self) -> MutexGuard<'_, Inner> {
        self.inner.lock().unwrap_or_else(|err| err.into_inner())
    }

    /// snapshot reads the state without holding the lock.
    pub fn snapshot(&self) -> UpdaterSnapshot {
        let inner = self.lock();
        self.snapshot_of(&inner)
    }

    /// snapshot_of reads the state behind a held lock.
    fn snapshot_of(&self, inner: &Inner) -> UpdaterSnapshot {
        let (supported, unsupported_reason) = match platform::can_self_update() {
            Ok(()) => (true, None),
            Err(reason) => (false, Some(reason)),
        };
        let available = inner
            .available
            .as_ref()
            .map(|release| release.summary.clone());
        let dismissed = available.as_ref().is_some_and(|summary| {
            inner.config.dismissed_version.as_deref() == Some(&summary.version)
        });
        UpdaterSnapshot {
            current_version: current_version().to_string(),
            supported,
            unsupported_reason,
            config: inner.config.clone(),
            status: inner.status.clone(),
            available,
            dismissed,
        }
    }

    /// emit sends the snapshot to the window.
    fn emit(&self, app: &AppHandle) {
        let _ = app.emit(EVENT, self.snapshot());
    }

    /// config returns a copy of the settings.
    pub(crate) fn config(&self) -> UpdaterConfig {
        self.lock().config.clone()
    }

    /// wait_for_config waits until the settings change.
    pub(crate) async fn wait_for_config(&self) {
        self.config_changed.notified().await;
    }

    /// retry_delay returns the wait after a rate limit.
    pub(crate) fn retry_delay(&self, now_unix_ms: i64) -> Option<Duration> {
        retry_delay(self.lock().retry_at_unix_ms, now_unix_ms)
    }

    /// set_status stores a status and tells the window.
    fn set_status(&self, app: &AppHandle, status: UpdateStatus) {
        self.lock().status = status;
        self.emit(app);
    }

    /// save writes the current settings.
    fn save(&self) {
        let (config, dir) = {
            let inner = self.lock();
            (inner.config.clone(), self.data_dir.clone())
        };
        if let Some(dir) = dir
            && let Err(err) = config.save(&dir)
        {
            crate::journal::error(&err);
        }
    }

    /// background_check runs a check of the scheduler.
    pub(crate) async fn background_check(&self, app: &AppHandle) {
        let _ = self.check(app, false).await;
    }

    /// check reads the releases and stores the result.
    async fn check(&self, app: &AppHandle, manual: bool) -> Result<UpdaterSnapshot, String> {
        if self.lock().status.busy() {
            return Ok(self.snapshot());
        }
        let current = match Version::parse(current_version()) {
            Ok(current) => current,
            Err(err) => {
                let message = format!("this build has no comparable version: {err}");
                self.set_status(
                    app,
                    UpdateStatus::Error {
                        code: "version_invalid".to_string(),
                        message: message.clone(),
                    },
                );
                return Err(message);
            }
        };
        self.set_status(app, UpdateStatus::Checking);

        let config = self.config();
        let outcome = github::check(
            &self.http,
            &api_url(),
            &config,
            &current,
            manual,
            platform::asset_match,
        )
        .await;

        let mut inner = self.lock();
        inner.config.last_checked_unix_ms = Some(now_unix_ms());
        match outcome {
            Ok(outcome) => {
                if let Some(etag) = outcome.etag {
                    inner.config.last_etag = Some(etag);
                }
                if !outcome.not_modified {
                    inner.available = outcome.release;
                }
                inner.retry_at_unix_ms = None;
                inner.status = if inner.available.is_some() {
                    UpdateStatus::Available
                } else {
                    UpdateStatus::UpToDate
                };
            }
            Err(err) => {
                // A failed check keeps the release it found before, so the
                // window can still offer it after the network comes back.
                inner.retry_at_unix_ms = err.retry_after_unix_ms;
                inner.status = UpdateStatus::Error {
                    code: err.code,
                    message: err.message,
                };
            }
        }
        let snapshot = self.snapshot_of(&inner);
        drop(inner);
        self.save();
        self.emit(app);
        Ok(snapshot)
    }

    /// install downloads, verifies and installs the offered release.
    async fn install(&self, app: &AppHandle) -> Result<(), String> {
        let release = {
            let inner = self.lock();
            if inner.status.busy() {
                return Err("an update is already in progress".to_string());
            }
            let Some(release) = inner.available.clone() else {
                return Err("there is no update to install".to_string());
            };
            release
        };

        let key = match verify::public_key() {
            Ok(key) => key,
            Err(err) => {
                self.set_status(
                    app,
                    UpdateStatus::Error {
                        code: err.code,
                        message: err.message.clone(),
                    },
                );
                return Err(err.message);
            }
        };
        let root = match verify::staging_root(app) {
            Ok(root) => root,
            Err(err) => {
                self.set_status(
                    app,
                    UpdateStatus::Error {
                        code: err.code,
                        message: err.message.clone(),
                    },
                );
                return Err(err.message);
            }
        };

        let cancel = Arc::new(AtomicBool::new(false));
        {
            let mut inner = self.lock();
            inner.cancel = Some(Arc::clone(&cancel));
            inner.status = UpdateStatus::Downloading {
                downloaded: 0,
                total: release.summary.asset_size,
            };
        }
        self.emit(app);

        let download = verify::download_and_verify(
            &self.http,
            &root,
            &release,
            &key,
            &cancel,
            |downloaded, total| {
                let mut inner = self.lock();
                // A cancelled download reports no progress of a running one.
                if matches!(inner.status, UpdateStatus::Downloading { .. }) {
                    inner.status = UpdateStatus::Downloading { downloaded, total };
                    drop(inner);
                    self.emit(app);
                }
            },
        )
        .await;
        self.lock().cancel = None;

        let artifact = match download {
            Ok(path) => path,
            Err(err) => {
                let status = if err.code == "cancelled" {
                    if self.lock().available.is_some() {
                        UpdateStatus::Available
                    } else {
                        UpdateStatus::Idle
                    }
                } else {
                    UpdateStatus::Error {
                        code: err.code,
                        message: err.message.clone(),
                    }
                };
                self.set_status(app, status);
                return Err(err.message);
            }
        };

        self.set_status(app, UpdateStatus::Verifying);
        let result = platform::install(app, &artifact, &release.summary.version, None, &|phase| {
            self.set_status(app, UpdateStatus::Installing { phase });
        })
        .await;

        match result {
            Ok(()) => {
                self.set_status(app, UpdateStatus::Restarting);
                Ok(())
            }
            Err(err) => {
                self.set_status(
                    app,
                    UpdateStatus::Error {
                        code: "install_failed".to_string(),
                        message: err.clone(),
                    },
                );
                Err(err)
            }
        }
    }
}

impl Default for Updater {
    fn default() -> Self {
        Self::new()
    }
}

/// retry_delay returns the wait until a retry time, or nothing without one.
fn retry_delay(retry_at_unix_ms: Option<i64>, now_unix_ms: i64) -> Option<Duration> {
    let retry = retry_at_unix_ms?;
    Some(if retry <= now_unix_ms {
        Duration::ZERO
    } else {
        Duration::from_millis((retry - now_unix_ms) as u64)
    })
}

/// current_version returns the version stamped into the binary.
pub fn current_version() -> &'static str {
    env!("UNISON_VERSION")
}

/// api_url returns the address of the releases. A debug build may point it at a
/// local server.
fn api_url() -> String {
    #[cfg(debug_assertions)]
    if let Ok(url) = std::env::var("UNISON_UPDATE_API_URL")
        && !url.trim().is_empty()
    {
        return url;
    }
    github::DEFAULT_API_URL.to_string()
}

/// now_unix_ms returns the current time in unix milliseconds.
fn now_unix_ms() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|elapsed| elapsed.as_millis() as i64)
        .unwrap_or_default()
}

/// run_arg hands an argument of the command line to the installer of the
/// platform. It returns nothing when an ordinary start runs.
pub fn run_arg() -> Option<i32> {
    platform::run_arg()
}

/// purge_stale removes the staging directories of previous runs.
pub fn purge_stale(app: &AppHandle) {
    if let Ok(root) = verify::staging_root(app) {
        verify::purge_stale(&root);
    }
}

/// updater_get_state returns the state of the updater.
#[tauri::command]
pub fn updater_get_state(updater: State<'_, Updater>) -> UpdaterSnapshot {
    updater.snapshot()
}

/// updater_set_config changes the settings of the updater.
#[tauri::command]
pub fn updater_set_config(
    app: AppHandle,
    updater: State<'_, Updater>,
    enabled: Option<bool>,
    beta_channel: Option<bool>,
    interval: Option<CheckInterval>,
) -> UpdaterSnapshot {
    {
        let mut inner = updater.lock();
        if let Some(enabled) = enabled {
            inner.config.enabled = enabled;
        }
        if let Some(beta_channel) = beta_channel {
            inner.config.set_beta_channel(beta_channel);
        }
        if let Some(interval) = interval {
            inner.config.interval = interval;
        }
    }
    updater.save();
    updater.config_changed.notify_one();
    updater.emit(&app);
    updater.snapshot()
}

/// updater_check_now checks for a release, even when the background checks are
/// off.
#[tauri::command]
pub async fn updater_check_now(
    app: AppHandle,
    updater: State<'_, Updater>,
) -> Result<UpdaterSnapshot, String> {
    updater.check(&app, true).await
}

/// updater_dismiss_update puts a version aside, which hides its banner until a
/// newer one appears.
#[tauri::command]
pub fn updater_dismiss_update(
    app: AppHandle,
    updater: State<'_, Updater>,
    version: String,
) -> UpdaterSnapshot {
    {
        let mut inner = updater.lock();
        inner.config.dismissed_version = Some(version);
    }
    updater.save();
    updater.emit(&app);
    updater.snapshot()
}

/// updater_install downloads, verifies and installs the offered release.
#[tauri::command]
pub async fn updater_install(app: AppHandle, updater: State<'_, Updater>) -> Result<(), String> {
    updater.install(&app).await
}

/// updater_cancel stops a running download. An installation that already
/// started cannot be stopped.
#[tauri::command]
pub fn updater_cancel(app: AppHandle, updater: State<'_, Updater>) -> Result<(), String> {
    let inner = updater.lock();
    if let Some(cancel) = &inner.cancel
        && inner.status.downloading()
    {
        cancel.store(true, Ordering::Relaxed);
        return Ok(());
    }
    drop(inner);
    let _ = app;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_status_serializes_with_its_kind() {
        let idle = serde_json::to_string(&UpdateStatus::Idle).expect("serialize");
        assert_eq!(idle, r#"{"kind":"idle"}"#);

        let downloading = serde_json::to_string(&UpdateStatus::Downloading {
            downloaded: 5,
            total: 10,
        })
        .expect("serialize");
        assert_eq!(
            downloading,
            r#"{"kind":"downloading","downloaded":5,"total":10}"#
        );

        let installing = serde_json::to_string(&UpdateStatus::Installing {
            phase: InstallPhase::Preparing,
        })
        .expect("serialize");
        assert_eq!(installing, r#"{"kind":"installing","phase":"preparing"}"#);
    }

    #[test]
    fn only_a_running_installation_is_busy() {
        assert!(UpdateStatus::Checking.busy());
        assert!(
            UpdateStatus::Downloading {
                downloaded: 0,
                total: 0
            }
            .busy()
        );
        assert!(UpdateStatus::Restarting.busy());
        assert!(!UpdateStatus::Idle.busy());
        assert!(!UpdateStatus::Available.busy());
        assert!(
            !UpdateStatus::Error {
                code: "network".to_string(),
                message: "down".to_string()
            }
            .busy()
        );
    }

    #[test]
    fn a_rate_limit_waits_for_its_reset() {
        assert_eq!(retry_delay(None, 1_000), None);
        assert_eq!(
            retry_delay(Some(3_000), 1_000),
            Some(Duration::from_millis(2_000))
        );
        assert_eq!(retry_delay(Some(1_000), 3_000), Some(Duration::ZERO));
    }

    #[test]
    fn a_cancel_stops_only_a_download() {
        assert!(
            UpdateStatus::Downloading {
                downloaded: 1,
                total: 2
            }
            .downloading()
        );
        assert!(UpdateStatus::Verifying.downloading());
        assert!(
            !UpdateStatus::Installing {
                phase: InstallPhase::ApplyingUpdate
            }
            .downloading()
        );
    }
}
