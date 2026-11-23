import { invoke } from "@tauri-apps/api/core";

/** openLocalNetworkSettings opens the Local Network pane of the privacy settings. */
export function openLocalNetworkSettings(): Promise<void> {
  return invoke("open_local_network_settings");
}
