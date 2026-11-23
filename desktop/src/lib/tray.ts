import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";

/** Event the tray icon sends to the webview. */
export const TRAY_ACTION_EVENT = "tray-action";

/** Pause mode the user picked in the tray. */
export type PauseChoice = "five_minutes" | "one_hour" | "indefinite" | "off";

/** Device the tray can send files to. */
export interface TrayDevice {
  id: string;
  name: string;
}

/** History entry the tray can copy back to the clipboard. */
export interface TrayRecentItem {
  id: string;
  label: string;
}

/** Payload of TRAY_ACTION_EVENT. */
export type TrayAction =
  | { action: "pause"; mode: PauseChoice }
  | { action: "copy_item"; id: string }
  | { action: "send_files"; deviceId: string; paths: string[] };

/** State the menu of the tray shows. */
export interface TrayState {
  connected: boolean;
  onlinePeers: number;
  paused: boolean;
  /** Online devices of the send menu. */
  devices: TrayDevice[];
  /** Most recent history entries, newest first. */
  recent: TrayRecentItem[];
}

let state: TrayState = {
  connected: false,
  onlinePeers: 0,
  paused: false,
  devices: [],
  recent: [],
};

/**
 * updateTray merges a part of the state into the menu. The shell, the history
 * and the transfers all report their own part, so the menu never shows the
 * state of one of them alone.
 */
export function updateTray(patch: Partial<TrayState>): Promise<void> {
  state = { ...state, ...patch };
  return invoke("set_tray_state", { state });
}

/** onTrayAction runs handler for every tray action. The returned function
 * stops the subscription. */
export function onTrayAction(handler: (action: TrayAction) => void): Promise<UnlistenFn> {
  return listen<TrayAction>(TRAY_ACTION_EVENT, (event) => handler(event.payload));
}
