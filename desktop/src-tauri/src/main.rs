// Hide the console window on Windows in release builds.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

fn main() {
    if let Some(code) = unison_desktop_lib::agent::run_arg() {
        std::process::exit(code);
    }
    unison_desktop_lib::run()
}
