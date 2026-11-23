import { invoke } from "@tauri-apps/api/core";

/**
 * showWindow brings the main window back from the tray or from behind the other
 * windows. A click on a notification uses it, since the notification of a
 * desktop may carry no buttons.
 */
export function showWindow(): Promise<void> {
  return invoke("show_window");
}
