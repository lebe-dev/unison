//! Reporting of the shell errors to Sentry.
//!
//! The DSN and the version are baked in at build time. Reporting stays off
//! until the user grants consent, and the events carry only the stack, the
//! release, the environment and the component tag.

use std::sync::OnceLock;
use std::sync::atomic::{AtomicBool, Ordering};

use sentry::ClientInitGuard;
use sentry::protocol::Event;

/// DSN is empty when the build has no Sentry DSN.
const DSN: &str = env!("UNISON_SENTRY_DSN");
/// VERSION is the version of the repository at build time.
const VERSION: &str = env!("UNISON_VERSION");
/// COMPONENT tags the events of the shell of the app.
const COMPONENT: &str = "desktop";

/// ENVIRONMENT names the kind of the build.
const ENVIRONMENT: &str = if cfg!(debug_assertions) {
    "debug"
} else {
    "release"
};

/// GUARD keeps the client alive for the lifetime of the process.
static GUARD: OnceLock<ClientInitGuard> = OnceLock::new();

/// ENABLED is the consent of the user, which is off by default.
static ENABLED: AtomicBool = AtomicBool::new(false);

/// init starts the SDK with the DSN baked into the build. A build without a
/// DSN reports nothing. The panic integration reports Rust panics and flushes
/// them before an abort of a release build.
pub fn init() {
    if DSN.is_empty() || DSN.parse::<sentry::types::Dsn>().is_err() || GUARD.get().is_some() {
        return;
    }
    let guard = sentry::init(
        sentry::ClientOptions::new()
            .dsn(DSN)
            .release(format!("unison@{VERSION}"))
            .environment(ENVIRONMENT)
            .attach_stacktrace(true)
            .send_default_pii(false)
            .before_send(before_send),
    );
    if guard.is_enabled() {
        let _ = GUARD.set(guard);
    }
}

/// set_enabled grants or withdraws the consent to report.
pub fn set_enabled(enabled: bool) {
    ENABLED.store(enabled, Ordering::Relaxed);
}

/// capture_error reports a fixed category of a critical error when the consent
/// is granted. The category must not carry details such as paths or addresses.
pub fn capture_error(category: &str) {
    if !ENABLED.load(Ordering::Relaxed) {
        return;
    }
    sentry::capture_message(category, sentry::Level::Error);
}

/// before_send drops the events without consent and keeps only what describes
/// the error of the shell.
fn before_send(mut event: Event<'static>) -> Option<Event<'static>> {
    if !ENABLED.load(Ordering::Relaxed) {
        return None;
    }
    event.user = None;
    event.server_name = None;
    event.request = None;
    event.breadcrumbs = Default::default();
    event.extra.clear();
    event.contexts.remove("device");
    event.contexts.remove("browser");
    event
        .tags
        .insert("component".to_string(), COMPONENT.to_string());
    Some(event)
}

#[cfg(test)]
mod tests {
    use super::*;
    use sentry::protocol::{Breadcrumb, Context, Request, User};
    use std::sync::Mutex;

    /// CONSENT serializes the tests that change the consent.
    static CONSENT: Mutex<()> = Mutex::new(());

    /// event returns an event with everything that must not be sent.
    fn event() -> Event<'static> {
        let mut event = Event {
            message: Some("the daemon did not start".to_string()),
            server_name: Some("somewhere".into()),
            user: Some(User {
                id: Some("someone".into()),
                ..Default::default()
            }),
            request: Some(Request::default()),
            breadcrumbs: vec![Breadcrumb {
                message: Some("a crumb".into()),
                ..Default::default()
            }]
            .into(),
            ..Default::default()
        };
        event.extra.insert("path".to_string(), "secret".into());
        event
            .contexts
            .insert("device".to_string(), Context::Device(Default::default()));
        event
            .contexts
            .insert("os".to_string(), Context::Os(Default::default()));
        event
    }

    #[test]
    fn events_are_dropped_without_consent() {
        let _consent = CONSENT.lock().unwrap_or_else(|err| err.into_inner());
        set_enabled(false);
        assert!(before_send(event()).is_none());
    }

    #[test]
    fn events_keep_only_the_error_and_the_component() {
        let _consent = CONSENT.lock().unwrap_or_else(|err| err.into_inner());
        set_enabled(true);
        let event = before_send(event()).expect("the event was dropped with consent");
        set_enabled(false);

        assert!(event.user.is_none());
        assert!(event.server_name.is_none());
        assert!(event.request.is_none());
        assert!(event.breadcrumbs.is_empty());
        assert!(event.extra.is_empty());
        assert!(!event.contexts.contains_key("device"));
        assert!(event.contexts.contains_key("os"));
        assert_eq!(
            event.tags.get("component").map(String::as_str),
            Some(COMPONENT)
        );
        assert_eq!(event.message.as_deref(), Some("the daemon did not start"));
    }
}
