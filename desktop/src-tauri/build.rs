use std::env;
use std::fs;
use std::path::Path;

fn main() {
    emit_build_env();
    emit_icon_watch();
    tauri_build::build();
}

/// emit_icon_watch rebuilds the app whenever an icon changes, because the
/// development Dock icon is embedded at compile time.
fn emit_icon_watch() {
    let manifest = env::var("CARGO_MANIFEST_DIR").expect("the manifest directory is set");
    let icons = Path::new(&manifest).join("icons");
    println!("cargo:rerun-if-changed={}", icons.display());
}

/// emit_build_env bakes the Sentry DSN and the version of the repository into
/// the binary. The variable of the environment wins over the `.env` file.
fn emit_build_env() {
    let manifest = env::var("CARGO_MANIFEST_DIR").expect("the manifest directory is set");
    let root = Path::new(&manifest).join("../..");
    let dotenv = root.join(".env");
    let version = root.join("VERSION");

    println!("cargo:rerun-if-changed={}", dotenv.display());
    println!("cargo:rerun-if-changed={}", version.display());
    println!("cargo:rerun-if-env-changed=SENTRY_DSN");

    let dsn = env::var("SENTRY_DSN")
        .ok()
        .or_else(|| dotenv_value(&dotenv, "SENTRY_DSN"))
        .unwrap_or_default();
    println!("cargo:rustc-env=UNISON_SENTRY_DSN={dsn}");

    let version = fs::read_to_string(&version)
        .map(|text| text.trim().to_string())
        .ok()
        .filter(|text| !text.is_empty())
        .unwrap_or_else(|| env::var("CARGO_PKG_VERSION").unwrap_or_default());
    println!("cargo:rustc-env=UNISON_VERSION={version}");
}

/// dotenv_value returns the value of a key in a `.env` file.
fn dotenv_value(path: &Path, key: &str) -> Option<String> {
    let text = fs::read_to_string(path).ok()?;
    text.lines().find_map(|line| {
        let line = line.trim();
        let line = line.strip_prefix("export ").unwrap_or(line);
        let (name, value) = line.split_once('=')?;
        (name.trim() == key).then(|| unquote(value.trim()).to_string())
    })
}

/// unquote removes the quotes around a value of a `.env` file.
fn unquote(value: &str) -> &str {
    for quote in ['"', '\''] {
        if let Some(inner) = value
            .strip_prefix(quote)
            .and_then(|rest| rest.strip_suffix(quote))
        {
            return inner;
        }
    }
    value
}
