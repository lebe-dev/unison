//! Settings of the updater, kept in `updater.json` of the data directory.

use std::fs;
use std::io::Write;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

/// Name of the settings file of the updater.
const FILE_NAME: &str = "updater.json";

/// How often the app checks GitHub for a release in the background.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum CheckInterval {
    SixHours,
    #[default]
    Daily,
    Weekly,
}

impl CheckInterval {
    /// seconds returns the length of the interval.
    pub fn seconds(self) -> u64 {
        match self {
            Self::SixHours => 6 * 60 * 60,
            Self::Daily => 24 * 60 * 60,
            Self::Weekly => 7 * 24 * 60 * 60,
        }
    }

    /// millis returns the length of the interval.
    pub fn millis(self) -> i64 {
        (self.seconds() * 1000) as i64
    }
}

/// UpdaterConfig is the persistent state of the updater.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct UpdaterConfig {
    /// Whether the app checks for a release in the background.
    pub enabled: bool,
    /// Whether GitHub pre-releases are candidates.
    pub beta_channel: bool,
    /// Interval of the background checks.
    pub interval: CheckInterval,
    /// Time of the last check that reached GitHub, in unix milliseconds.
    pub last_checked_unix_ms: Option<i64>,
    /// Version the user put aside, which hides its banner.
    pub dismissed_version: Option<String>,
    /// Entity tag of the last answer, which makes a later check cheaper.
    pub last_etag: Option<String>,
}

impl Default for UpdaterConfig {
    fn default() -> Self {
        Self {
            enabled: false,
            beta_channel: false,
            interval: CheckInterval::Daily,
            last_checked_unix_ms: None,
            dismissed_version: None,
            last_etag: None,
        }
    }
}

impl UpdaterConfig {
    /// path returns the file of the settings in the data directory.
    pub fn path(data_dir: &Path) -> PathBuf {
        data_dir.join(FILE_NAME)
    }

    /// load reads the settings. A missing file yields the defaults and a
    /// malformed one yields the defaults with a warning.
    pub fn load(data_dir: &Path) -> Self {
        let path = Self::path(data_dir);
        let text = match fs::read_to_string(&path) {
            Ok(text) => text,
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => return Self::default(),
            Err(err) => {
                crate::journal::record(
                    "WARN",
                    &format!("the settings of the updater cannot be read: {err}"),
                );
                return Self::default();
            }
        };
        match serde_json::from_str(&text) {
            Ok(config) => config,
            Err(err) => {
                crate::journal::record(
                    "WARN",
                    &format!("the settings of the updater are malformed: {err}"),
                );
                Self::default()
            }
        }
    }

    /// save writes the settings atomically, reachable only by the user.
    pub fn save(&self, data_dir: &Path) -> Result<(), String> {
        fs::create_dir_all(data_dir)
            .map_err(|err| format!("cannot create the data directory: {err}"))?;
        let text = serde_json::to_vec_pretty(self)
            .map_err(|err| format!("cannot write the settings of the updater: {err}"))?;
        let path = Self::path(data_dir);
        let temporary = data_dir.join(format!("{FILE_NAME}.tmp"));
        let mut file = fs::File::create(&temporary)
            .map_err(|err| format!("cannot write the settings of the updater: {err}"))?;
        file.write_all(&text)
            .and_then(|()| file.sync_all())
            .map_err(|err| format!("cannot write the settings of the updater: {err}"))?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            fs::set_permissions(&temporary, fs::Permissions::from_mode(0o600))
                .map_err(|err| format!("cannot restrict the settings of the updater: {err}"))?;
        }
        fs::rename(&temporary, &path)
            .map_err(|err| format!("cannot write the settings of the updater: {err}"))
    }

    /// set_beta_channel switches the channel and forgets the state that belongs
    /// to the previous one, so the next check starts clean.
    pub fn set_beta_channel(&mut self, beta_channel: bool) {
        if self.beta_channel == beta_channel {
            return;
        }
        self.beta_channel = beta_channel;
        self.last_etag = None;
        self.dismissed_version = None;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::testutil::TempDir;

    #[test]
    fn defaults_are_off_and_daily() {
        let config = UpdaterConfig::default();
        assert!(!config.enabled);
        assert!(!config.beta_channel);
        assert_eq!(config.interval, CheckInterval::Daily);
        assert_eq!(CheckInterval::SixHours.seconds(), 21_600);
        assert_eq!(CheckInterval::Weekly.seconds(), 604_800);
    }

    #[test]
    fn round_trips_through_json() {
        let dir = TempDir::new();
        let config = UpdaterConfig {
            enabled: true,
            beta_channel: true,
            interval: CheckInterval::Weekly,
            last_checked_unix_ms: Some(1_700_000_000_000),
            dismissed_version: Some("0.3.0".to_string()),
            last_etag: Some("\"abc\"".to_string()),
        };
        config.save(dir.path()).expect("save");
        assert_eq!(UpdaterConfig::load(dir.path()), config);
    }

    #[test]
    fn a_missing_file_yields_the_defaults() {
        let dir = TempDir::new();
        assert_eq!(UpdaterConfig::load(dir.path()), UpdaterConfig::default());
    }

    #[test]
    fn a_corrupt_file_yields_the_defaults() {
        let dir = TempDir::new();
        fs::write(UpdaterConfig::path(dir.path()), b"{not json").expect("write");
        assert_eq!(UpdaterConfig::load(dir.path()), UpdaterConfig::default());
    }

    #[test]
    fn a_partial_file_keeps_the_defaults_of_the_missing_fields() {
        let dir = TempDir::new();
        fs::write(UpdaterConfig::path(dir.path()), br#"{"enabled":true}"#).expect("write");
        let config = UpdaterConfig::load(dir.path());
        assert!(config.enabled);
        assert_eq!(config.interval, CheckInterval::Daily);
    }

    #[test]
    fn switching_the_channel_clears_the_cached_state() {
        let mut config = UpdaterConfig {
            last_etag: Some("\"abc\"".to_string()),
            dismissed_version: Some("0.3.0".to_string()),
            ..UpdaterConfig::default()
        };
        config.set_beta_channel(false);
        assert!(
            config.last_etag.is_some(),
            "an unchanged channel kept nothing"
        );
        config.set_beta_channel(true);
        assert_eq!(config.last_etag, None);
        assert_eq!(config.dismissed_version, None);
    }
}
