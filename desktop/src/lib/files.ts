import { invoke } from "@tauri-apps/api/core";

/**
 * pickFiles opens the native file dialog and returns the absolute paths the
 * user picked. An empty list means the dialog was closed.
 */
export function pickFiles(): Promise<string[]> {
  return invoke<string[]>("pick_files");
}

/**
 * pickFolder opens the native folder dialog and returns the absolute path the
 * user picked, or null when the dialog was closed.
 */
export function pickFolder(): Promise<string | null> {
  return invoke<string | null>("pick_folder");
}
