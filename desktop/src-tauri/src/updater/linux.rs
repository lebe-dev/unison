//! Installation of an update on Linux.
//!
//! The installer of this platform is added by its own change. Until then the
//! updater reports the platform as unsupported.

use std::path::Path;

use tauri::AppHandle;

use super::InstallPhase;

/// asset_match reports whether a file of a release is the artifact of Linux.
pub fn asset_match(filename: &str, version: &str) -> bool {
    filename == format!("unison-v{version}-linux.deb")
}

/// can_self_update reports why the app cannot replace itself here.
pub fn can_self_update() -> Result<(), String> {
    Err("the Linux installer of updates is not implemented".to_string())
}

/// run_arg handles the flags the installer starts the app with.
pub fn run_arg() -> Option<i32> {
    None
}

/// install replaces the app with the downloaded artifact.
pub async fn install(
    _app: &AppHandle,
    _artifact_path: &Path,
    _expected_version: &str,
    _endpoint: Option<&str>,
    _on_phase: &(dyn Fn(InstallPhase) + Send + Sync),
) -> Result<(), String> {
    Err("the Linux installer of updates is not implemented".to_string())
}
