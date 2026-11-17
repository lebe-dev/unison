import { invoke } from "@tauri-apps/api/core";

/** setSentryEnabled grants or withdraws the consent of the shell to report errors. */
export function setSentryEnabled(enabled: boolean): Promise<void> {
  return invoke("set_sentry_enabled", { enabled });
}
