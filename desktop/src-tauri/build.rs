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

/// emit_build_env bakes the Sentry DSN, the version of the repository and the
/// public key of the updates into the binary. A variable of the environment
/// wins over the `.env` file and the files of the repository.
fn emit_build_env() {
    let manifest = env::var("CARGO_MANIFEST_DIR").expect("the manifest directory is set");
    let root = Path::new(&manifest).join("../..");
    let dotenv = root.join(".env");
    let version_file = root.join("VERSION");

    println!("cargo:rerun-if-changed={}", dotenv.display());
    println!("cargo:rerun-if-changed={}", version_file.display());
    println!("cargo:rerun-if-env-changed=SENTRY_DSN");
    println!("cargo:rerun-if-env-changed=UNISON_VERSION");
    println!("cargo:rerun-if-env-changed=UNISON_UPDATE_PUBLIC_KEY");

    let dsn = env::var("SENTRY_DSN")
        .ok()
        .or_else(|| dotenv_value(&dotenv, "SENTRY_DSN"))
        .unwrap_or_default();
    println!("cargo:rustc-env=UNISON_SENTRY_DSN={dsn}");

    // A release build names its exact version, including a pre-release suffix,
    // so the updater compares the version that runs and not the crate version.
    let version = env::var("UNISON_VERSION")
        .ok()
        .map(|text| text.trim().to_string())
        .filter(|text| !text.is_empty())
        .or_else(|| read_trimmed(&version_file))
        .unwrap_or_else(|| env::var("CARGO_PKG_VERSION").unwrap_or_default());
    println!("cargo:rustc-env=UNISON_VERSION={version}");

    let key = public_key(&root);
    println!("cargo:rustc-env=UNISON_UPDATE_PUBLIC_KEY={key}");
}

/// read_trimmed returns the trimmed text of a file when it is not empty.
fn read_trimmed(path: &Path) -> Option<String> {
    fs::read_to_string(path)
        .ok()
        .map(|text| text.trim().to_string())
        .filter(|text| !text.is_empty())
}

/// public_key returns the key that verifies a release. The variable of the
/// environment wins, so a build for a test can use its own key.
fn public_key(root: &Path) -> String {
    if let Ok(key) = env::var("UNISON_UPDATE_PUBLIC_KEY")
        && !key.trim().is_empty()
    {
        return key.trim().to_string();
    }
    let path = root.join("packaging/release-public-key.txt");
    println!("cargo:rerun-if-changed={}", path.display());
    read_trimmed(&path).unwrap_or_default()
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
