import { invoke } from "@tauri-apps/api/core";
import { client } from "./api/client";

/** setSentryEnabled grants or withdraws the consent of the shell to report errors. */
export function setSentryEnabled(enabled: boolean): Promise<void> {
  return invoke("set_sentry_enabled", { enabled });
}

/**
 * SentrySync passes the consent that the daemon stores to the shell of the
 * app. The shell reads it once when the daemon answers, and again after a read
 * that failed.
 */
export class SentrySync {
  #known = false;
  #pending = false;

  /** sync reads the consent unless the shell already knows it. */
  async sync(): Promise<void> {
    if (this.#known || this.#pending) {
      return;
    }
    this.#pending = true;
    try {
      const response = await client.getSettings({});
      await setSentryEnabled(response.settings?.sentryEnabled ?? false);
      this.#known = true;
    } catch (error) {
      // The shell keeps its default and the next attempt reads the setting.
      console.warn("cannot read the error reporting setting", error);
    } finally {
      this.#pending = false;
    }
  }
}
