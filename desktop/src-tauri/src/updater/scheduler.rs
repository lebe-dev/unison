//! The background checks of the updater.
//!
//! The loop sleeps until the configured interval passed and starts immediately
//! when the settings change. It never downloads or installs anything: a found
//! release only wakes the window through the state event.

use std::time::Duration;

use tauri::{AppHandle, Manager};

use super::Updater;
use super::config::UpdaterConfig;

/// Delay of the first check after a start, so the daemon starts first.
pub const STARTUP_DELAY: Duration = Duration::from_secs(30);

/// delay_until_check returns how long the loop waits before the next check.
/// `None` means that background checks are off.
pub fn delay_until_check(config: &UpdaterConfig, now_unix_ms: i64) -> Option<Duration> {
    if !config.enabled {
        return None;
    }
    let Some(last) = config.last_checked_unix_ms else {
        // A fresh start waits once, so the boot of the app stays untouched.
        return Some(STARTUP_DELAY);
    };
    let due = last.saturating_add(config.interval.millis());
    if due <= now_unix_ms {
        return Some(Duration::ZERO);
    }
    Some(Duration::from_millis((due - now_unix_ms) as u64))
}

/// run keeps the background checks of the app alive.
pub async fn run(app: AppHandle) {
    let mut first = true;
    loop {
        let updater = app.state::<Updater>();
        let wait = {
            let config = updater.config();
            let now = now_unix_ms();
            if !config.enabled {
                None
            } else if first {
                Some(STARTUP_DELAY)
            } else if let Some(wait) = updater.retry_delay(now) {
                Some(wait)
            } else {
                delay_until_check(&config, now)
            }
        };
        first = false;

        let Some(wait) = wait else {
            updater.wait_for_config().await;
            continue;
        };
        tokio::select! {
            () = tokio::time::sleep(wait) => {}
            () = updater.wait_for_config() => continue,
        }
        updater.background_check(&app).await;
    }
}

/// now_unix_ms returns the current time in unix milliseconds.
fn now_unix_ms() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|elapsed| elapsed.as_millis() as i64)
        .unwrap_or_default()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::updater::config::CheckInterval;

    #[test]
    fn disabled_checks_never_wait() {
        assert_eq!(delay_until_check(&UpdaterConfig::default(), 0), None);
    }

    #[test]
    fn a_fresh_start_waits_for_the_startup_delay() {
        let config = UpdaterConfig {
            enabled: true,
            ..UpdaterConfig::default()
        };
        assert_eq!(delay_until_check(&config, 1_000), Some(STARTUP_DELAY));
    }

    #[test]
    fn a_recent_check_waits_for_the_rest_of_the_interval() {
        let config = UpdaterConfig {
            enabled: true,
            last_checked_unix_ms: Some(1_000_000),
            ..UpdaterConfig::default()
        };
        assert_eq!(
            delay_until_check(&config, 1_000_000 + 60_000),
            Some(Duration::from_millis(
                CheckInterval::Daily.millis() as u64 - 60_000
            ))
        );
    }

    #[test]
    fn an_overdue_check_runs_at_once() {
        let config = UpdaterConfig {
            enabled: true,
            last_checked_unix_ms: Some(1_000_000),
            ..UpdaterConfig::default()
        };
        assert_eq!(
            delay_until_check(&config, 9_999_999_999),
            Some(Duration::ZERO)
        );
    }

    #[test]
    fn a_longer_interval_waits_longer() {
        let config = UpdaterConfig {
            enabled: true,
            last_checked_unix_ms: Some(1_000_000),
            ..UpdaterConfig::default()
        };
        let daily = delay_until_check(&config, 1_000_000).expect("a wait");
        let weekly = delay_until_check(
            &UpdaterConfig {
                interval: CheckInterval::Weekly,
                ..config
            },
            1_000_000,
        )
        .expect("a wait");
        assert!(weekly > daily);
    }
}
