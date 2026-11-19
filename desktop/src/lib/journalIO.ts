import { invoke } from "@tauri-apps/api/core";
import { writeText as writeClipboard } from "@tauri-apps/plugin-clipboard-manager";
import { save } from "@tauri-apps/plugin-dialog";
import { showInFolder } from "./files";

/** Info of the shell and the system that the export header carries. */
export interface ShellInfo {
  appVersion: string;
  os: string;
}

/** Export file the shell wrote in the temporary directory. */
export interface SavedLog {
  name: string;
  path: string;
}

/** shellInfo returns the version of the app and the version of the system. */
export function shellInfo(): Promise<ShellInfo> {
  return invoke<ShellInfo>("journal_shell_info");
}

/** journalEnabled reports whether the marker keeps the journal on. */
export function journalEnabled(): Promise<boolean> {
  return invoke<boolean>("journal_enabled");
}

/** readLines returns the current records of the shell and the daemon, oldest
 * first. The display does not need the rotated files. */
export function readLines(): Promise<string[]> {
  return invoke<string[]>("journal_read");
}

/** readAllLines returns every record, including the rotated files. */
export function readAllLines(): Promise<string[]> {
  return invoke<string[]>("journal_read_all");
}

/** recordFailure writes a fixed failure of the window to the application log. */
export function recordFailure(message: string): void {
  void invoke("journal_record", { level: "ERROR", message }).catch(() => {
    // The log must never break the action that failed.
  });
}

/** writeLog writes the export to a temporary file and returns it. */
export function writeLog(text: string): Promise<SavedLog> {
  return invoke<SavedLog>("journal_write", { text });
}

/** saveLog copies the export to a path the user picked. */
export function saveLog(source: string, target: string): Promise<void> {
  return invoke("journal_save", { source, target });
}

/** copyText puts the export on the clipboard. */
export async function copyText(text: string): Promise<void> {
  try {
    await writeClipboard(text);
  } catch (error) {
    // A webview without the plugin permission may still have the browser API.
    if (!navigator.clipboard?.writeText) {
      throw error;
    }
    await navigator.clipboard.writeText(text);
  }
}

/**
 * shareLog opens the share sheet of the system with the export file. When the
 * sheet cannot be shown, the user saves the file with the save dialog and the
 * file manager reveals it.
 */
export async function shareLog(log: SavedLog): Promise<void> {
  try {
    await invoke("journal_share", { path: log.path });
    return;
  } catch {
    // The native share sheet is unavailable, so the file takes the save path.
  }
  const target = await save({
    defaultPath: log.name,
    filters: [{ name: "Log", extensions: ["log"] }],
  });
  if (!target) {
    return;
  }
  await saveLog(log.path, target);
  await showInFolder([target]);
}
