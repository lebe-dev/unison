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

/// PATH_PLACEHOLDER marks where an absolute path was removed.
const PATH_PLACEHOLDER: &str = "[path]";

/// UNIX_ROOTS are the roots of the absolute paths that the events report.
const UNIX_ROOTS: [&str; 10] = [
    "/Users/",
    "/home/",
    "/root/",
    "/tmp/",
    "/private/",
    "/var/",
    "/opt/",
    "/mnt/",
    "/data/",
    "/storage/",
];

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
    if let Some(message) = event.message.take() {
        event.message = Some(redact_paths(&message));
    }
    for exception in &mut event.exception.values {
        if let Some(value) = exception.value.take() {
            exception.value = Some(redact_paths(&value));
        }
    }
    Some(event)
}

/// redact_paths replaces the absolute paths of a text with a placeholder, so
/// the reports never carry the files of the user. The text is split into
/// whitespace separated tokens, which keeps ordinary words and URLs.
fn redact_paths(text: &str) -> String {
    let mut redacted = String::with_capacity(text.len());
    for part in text.split_inclusive(char::is_whitespace) {
        let end = part.trim_end_matches(char::is_whitespace).len();
        let (token, space) = part.split_at(end);
        redacted.push_str(&redact_token(token));
        redacted.push_str(space);
    }
    redacted
}

/// redact_token replaces the path of a token and keeps the punctuation around
/// it.
fn redact_token(token: &str) -> String {
    let start = token
        .char_indices()
        .find(|(_, c)| !matches!(c, '(' | '[' | '{' | '\'' | '"' | '<'))
        .map_or(0, |(index, _)| index);
    let rest = &token[start..];
    if !is_path(rest) {
        return token.to_string();
    }
    let end = rest
        .char_indices()
        .rev()
        .find(|(_, c)| !matches!(c, ')' | ']' | '}' | ',' | ';' | ':' | '\'' | '"' | '>'))
        .map_or(rest.len(), |(index, c)| index + c.len_utf8());
    format!("{}{PATH_PLACEHOLDER}{}", &token[..start], &rest[end..])
}

/// is_path reports whether a token starts an absolute path of the computer.
fn is_path(token: &str) -> bool {
    UNIX_ROOTS.iter().any(|root| token.starts_with(root)) || is_windows_path(token)
}

/// is_windows_path reports whether a token starts a path with a drive letter.
fn is_windows_path(token: &str) -> bool {
    let mut chars = token.chars();
    matches!(chars.next(), Some(letter) if letter.is_ascii_alphabetic())
        && chars.next() == Some(':')
        && matches!(chars.next(), Some('\\') | Some('/'))
}

#[cfg(test)]
mod tests {
    use super::*;
    use sentry::protocol::{Breadcrumb, Context, Exception, Request, User};
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

    #[test]
    fn redaction_covers_a_unix_path() {
        assert_eq!(
            redact_paths("cannot open /tmp/unison/build.log now"),
            "cannot open [path] now"
        );
        assert_eq!(
            redact_paths("the store at /var/lib/unison is busy"),
            "the store at [path] is busy"
        );
    }

    #[test]
    fn redaction_covers_a_macos_home() {
        assert_eq!(
            redact_paths("the file (/Users/someone/Documents/report.pdf) is missing"),
            "the file ([path]) is missing"
        );
    }

    #[test]
    fn redaction_covers_windows_drives() {
        assert_eq!(
            redact_paths(r"cannot open C:\Users\Someone\AppData\Local\Unison\unison.log"),
            "cannot open [path]"
        );
        assert_eq!(
            redact_paths("cannot open D:/data/transfers/report.pdf"),
            "cannot open [path]"
        );
    }

    #[test]
    fn redaction_leaves_words_and_urls_alone() {
        let text = "the home page at https://example.com/home/page is open";
        assert_eq!(redact_paths(text), text);
        assert_eq!(
            redact_paths("the daemon did not start"),
            "the daemon did not start"
        );
    }

    #[test]
    fn events_lose_the_paths_of_the_user() {
        let _consent = CONSENT.lock().unwrap_or_else(|err| err.into_inner());
        set_enabled(true);
        let mut event = event();
        event.message = Some("cannot reach the daemon at /Users/someone/Library".to_string());
        event.exception = vec![
            Exception {
                ty: "panic".to_string(),
                value: Some("at /home/someone/unison/src/main.rs".to_string()),
                ..Default::default()
            },
            Exception {
                ty: "cause".to_string(),
                value: Some(r"the file C:\Users\Someone\report.txt is missing".to_string()),
                ..Default::default()
            },
        ]
        .into();
        let event = before_send(event).expect("the event was dropped with consent");
        set_enabled(false);

        assert_eq!(
            event.message.as_deref(),
            Some("cannot reach the daemon at [path]")
        );
        assert_eq!(
            event.exception.values[0].value.as_deref(),
            Some("at [path]")
        );
        assert_eq!(
            event.exception.values[1].value.as_deref(),
            Some("the file [path] is missing")
        );
    }
}
