import { invoke } from "@tauri-apps/api/core";
import { open, save } from "@tauri-apps/plugin-dialog";

/** pickSettingsTarget opens the save dialog and returns the chosen path, or
 * null when the dialog was closed. */
export async function pickSettingsTarget(defaultName: string): Promise<string | null> {
  const target = await save({
    defaultPath: defaultName,
    filters: [{ name: "Settings", extensions: ["json"] }],
  });
  return typeof target === "string" ? target : null;
}

/** pickSettingsSource opens the open dialog and returns the chosen path, or
 * null when the dialog was closed. */
export async function pickSettingsSource(): Promise<string | null> {
  const picked = await open({
    multiple: false,
    directory: false,
    filters: [{ name: "Settings", extensions: ["json"] }],
  });
  return typeof picked === "string" ? picked : null;
}

/** writeSettingsFile writes the document to the chosen path. */
export function writeSettingsFile(path: string, text: string): Promise<void> {
  return invoke("settings_write", { path, text });
}

/** readSettingsFile returns the document of the chosen path. */
export function readSettingsFile(path: string): Promise<string> {
  return invoke<string>("settings_read", { path });
}
