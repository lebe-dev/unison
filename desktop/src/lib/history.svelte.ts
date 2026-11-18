import { client } from "./api/client";
import {
  EventKind,
  Kind,
  type Event as DaemonEvent,
  type HistoryItem,
} from "./api/gen/unison/local/v1/local_pb";
import { daemon } from "./daemon.svelte";
import { errorText } from "./errors";
import { historyTitle, kindLabel } from "./format";
import { translate } from "./i18n";
import { updateTray, type TrayRecentItem } from "./tray";

/** Items a screen asks for at once. The daemon caps the number itself. */
export const HISTORY_LIMIT = 200;

/** Entries of the recent items menu of the tray. */
export const TRAY_LIMIT = 5;

/** Refresh of the tray entries, which covers items received while the window
 * was hidden. */
const RECENT_INTERVAL = 20_000;

/** Message for the user after an action of the screen. */
export interface HistoryNotice {
  kind: "info" | "error";
  text: string;
}

/**
 * HistoryStore keeps the clip history and the recent items of the tray. The
 * history is kept by the daemon, so the store follows the events and reloads
 * the list, which is bounded by HISTORY_LIMIT.
 */
class HistoryStore {
  /** Items of the current filter, newest first. */
  items = $state<HistoryItem[]>([]);
  /** Kind filter, KIND_UNSPECIFIED keeps every kind. */
  kind = $state<Kind>(Kind.UNSPECIFIED);
  /** Device that captured the items, lowercase hex. Empty keeps every device. */
  origin = $state("");
  /** A reload of the list is in flight. */
  loading = $state(false);
  /** Reason of the last failure of the list, absent when it works. */
  error = $state<string | null>(null);
  /** Message of the last action. */
  notice = $state<HistoryNotice | null>(null);
  /** Directory that receives downloaded files. */
  downloadsDir = $state("");
  /** Items with a download in flight. */
  downloading = $state<string[]>([]);
  /** Items that wait for the user to apply them. */
  held = $state<string[]>([]);

  #started = false;
  #unsubscribe: (() => void) | null = null;
  #timer: ReturnType<typeof setInterval> | null = null;
  #loading = false;

  /** start follows the events and keeps the tray entries fresh. */
  start(): void {
    if (this.#started) {
      return;
    }
    this.#started = true;
    this.#unsubscribe = daemon.onEvent((event) => this.#onEvent(event));
    this.#timer = setInterval(() => void this.refreshRecent(), RECENT_INTERVAL);
    void this.loadSettings();
    void this.load();
  }

  /** stop releases the subscriptions of the store. */
  stop(): void {
    this.#started = false;
    this.#unsubscribe?.();
    this.#unsubscribe = null;
    if (this.#timer) {
      clearInterval(this.#timer);
      this.#timer = null;
    }
  }

  /** load reloads the list with the current filter. A quiet reload keeps the
   * list on screen while the request is in flight. */
  async load(quiet = false): Promise<void> {
    if (this.#loading) {
      return;
    }
    this.#loading = true;
    if (!quiet) {
      this.loading = true;
    }
    try {
      const response = await client.listHistory({
        kind: this.kind,
        deviceId: this.origin,
        limit: HISTORY_LIMIT,
      });
      this.items = response.items;
      this.error = null;
    } catch (error) {
      this.error = errorText(error, translate("history.loadFailed"));
    } finally {
      this.#loading = false;
      this.loading = false;
    }
  }

  /** setKind applies a kind filter and reloads the list. */
  async setKind(kind: Kind): Promise<void> {
    this.kind = kind;
    await this.load();
  }

  /** setOrigin applies a device filter and reloads the list. */
  async setOrigin(origin: string): Promise<void> {
    this.origin = origin;
    await this.load();
  }

  /** copy writes an item back to the local clipboard. */
  async copy(id: string): Promise<void> {
    this.notice = null;
    try {
      await client.copyHistoryItem({ id });
      this.release(id);
      this.notice = { kind: "info", text: translate("history.onClipboard") };
    } catch (error) {
      this.notice = { kind: "error", text: errorText(error, translate("history.copyFailed")) };
    }
  }

  /**
   * apply writes an item that waited for confirmation to the local clipboard.
   * It is what the notification of a held item and the Apply button do.
   */
  async apply(id: string): Promise<void> {
    this.notice = null;
    try {
      await client.applyHistoryItem({ id });
      this.release(id);
      this.notice = { kind: "info", text: translate("history.onClipboard") };
    } catch (error) {
      this.notice = { kind: "error", text: errorText(error, translate("history.applyFailed")) };
    }
  }

  /**
   * download stores the payloads of an item on this device. Files are written
   * to the downloads directory of the daemon, other kinds only fill the local
   * store.
   */
  async download(item: HistoryItem): Promise<void> {
    const info = item.item;
    if (!info) {
      return;
    }
    if (this.downloading.includes(info.id)) {
      return;
    }
    this.notice = null;
    this.downloading = [...this.downloading, info.id];
    try {
      const path = info.kind === Kind.FILES ? this.downloadsDir : "";
      const response = await client.fetchHistoryItem({ id: info.id, path });
      this.#replace(response.item ?? item);
      if (response.files.length > 0) {
        const saved = response.files.join(", ");
        this.notice = {
          kind: "info",
          text: translate("history.saved", { paths: saved }),
        };
      } else {
        this.notice = {
          kind: "info",
          text: translate("history.downloadedLocal"),
        };
      }
      void this.refreshRecent();
    } catch (error) {
      this.notice = {
        kind: "error",
        text: errorText(error, translate("history.downloadFailed")),
      };
    } finally {
      this.downloading = this.downloading.filter((id) => id !== info.id);
    }
  }

  /** remove deletes an item on every device. */
  async remove(id: string): Promise<void> {
    this.notice = null;
    try {
      await client.deleteHistoryItem({ id });
      this.items = this.items.filter((item) => item.item?.id !== id);
      this.release(id);
      this.notice = { kind: "info", text: translate("history.deleted") };
      void this.refreshRecent();
    } catch (error) {
      this.notice = { kind: "error", text: errorText(error, translate("history.deleteFailed")) };
    }
  }

  /** clear removes every item of the history on every device. */
  async clear(): Promise<void> {
    this.notice = null;
    try {
      const response = await client.clearHistory({});
      this.items = [];
      this.held = [];
      this.notice = {
        kind: "info",
        text: translate("history.cleared", { count: Number(response.removed) }),
      };
      void this.refreshRecent();
    } catch (error) {
      this.notice = { kind: "error", text: errorText(error, translate("history.clearFailed")) };
    }
  }

  /** refreshRecent reloads the entries of the tray menu. */
  async refreshRecent(): Promise<void> {
    try {
      const response = await client.listHistory({ limit: TRAY_LIMIT });
      const recent: TrayRecentItem[] = response.items.slice(0, TRAY_LIMIT).map((item) => ({
        id: item.item?.id ?? "",
        label: trayLabel(item),
      }));
      await updateTray({ recent: recent.filter((item) => item.id !== "") });
    } catch {
      // The tray keeps the entries it has; the next event retries.
    }
  }

  /** loadSettings reads the downloads directory. */
  async loadSettings(): Promise<void> {
    try {
      const response = await client.getSettings({});
      this.downloadsDir = response.settings?.downloadsDir ?? "";
    } catch {
      // The download falls back to the daemon directory.
    }
  }

  #onEvent(event: DaemonEvent): void {
    switch (event.kind) {
      case EventKind.CLIPBOARD:
      case EventKind.CLIP_DOWNLOAD:
        void this.load(true);
        void this.refreshRecent();
        break;
      case EventKind.CLIP_APPLY:
        // The item is in the history and waits for the user to apply it.
        if (event.item && !this.held.includes(event.item.id)) {
          this.held = [event.item.id, ...this.held];
        }
        void this.load(true);
        void this.refreshRecent();
        break;
      case EventKind.PEER_ONLINE:
        void this.load(true);
        void this.refreshRecent();
        break;
    }
  }

  /** release forgets an item that is no longer held back. The item stays in
   * the history. */
  release(id: string): void {
    this.held = this.held.filter((current) => current !== id);
  }

  /** #replace updates one item of the list after a call. */
  #replace(item: HistoryItem): void {
    this.items = this.items.map((current) => (current.item?.id === item.item?.id ? item : current));
  }
}

/** trayLabel returns the menu text of a history entry. */
function trayLabel(item: HistoryItem): string {
  const info = item.item;
  if (!info) {
    return "";
  }
  const title = historyTitle(info);
  if (info.kind === Kind.TEXT) {
    return title;
  }
  return `${kindLabel(info.kind)}: ${title}`;
}

/** history is the history state of the app. */
export const history = new HistoryStore();
