import { invoke } from "@tauri-apps/api/core";

/** State of the login item that starts the daemon. */
export type LoginItemStatus =
  "unsupported" | "not_registered" | "enabled" | "requires_approval" | "not_found";

/** Answer of the login item commands. */
export interface LoginItem {
  status: LoginItemStatus;
}

/** loginItemStatus reads the state of the login item. */
export function loginItemStatus(): Promise<LoginItem> {
  return invoke("login_item_status");
}

/** setLoginItem registers or unregisters the login item and returns its state. */
export function setLoginItem(enabled: boolean): Promise<LoginItem> {
  return invoke("set_login_item", { enabled });
}
