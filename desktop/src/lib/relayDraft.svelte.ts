import type { Relay } from "./api/gen/unison/local/v1/local_pb";

/**
 * RelayDraft keeps the values of the relay form. A quiet refresh changes the
 * state of the relay only, so the values the user types live here and survive
 * a remount of the section.
 */
class RelayDraft {
  url = $state("");
  token = $state("");
  filesOnRequest = $state(true);
  /** ready reports a draft that holds the stored relay at least once. */
  ready = $state(false);

  /** fill takes the stored relay once, and again after an explicit change. */
  fill(relay: Relay | undefined, force = false): void {
    if (this.ready && !force) {
      return;
    }
    this.url = relay?.url ?? "";
    this.filesOnRequest = relay?.filesOnRequest ?? true;
    this.token = "";
    this.ready = true;
  }
}

/** relayDraft is the relay form of the settings screen. */
export const relayDraft = new RelayDraft();
