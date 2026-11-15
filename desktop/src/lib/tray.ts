import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";

/** Event the tray icon sends to the webview. */
export const TRAY_ACTION_EVENT = "tray-action";

/** Pause mode the user picked in the tray. */
export type PauseChoice = "five_minutes" | "one_hour" | "indefinite" | "off";

/** Payload of TRAY_ACTION_EVENT. */
export type TrayAction = { action: "pause"; mode: PauseChoice };

/** State the menu of the tray shows. */
export interface TrayState {
  connected: boolean;
  onlinePeers: number;
  paused: boolean;
}

/** syncTray updates the texts of the tray menu. */
export function syncTray(state: TrayState): Promise<void> {
  return invoke("set_tray_state", { state });
}

/** onTrayAction runs handler for every tray action. The returned function
 * stops the subscription. */
export function onTrayAction(handler: (action: TrayAction) => void): Promise<UnlistenFn> {
  return listen<TrayAction>(TRAY_ACTION_EVENT, (event) => handler(event.payload));
}
