import { client } from "./api/client";
import {
  EventKind,
  Kind,
  type Event as DaemonEvent,
  type HistoryItem,
} from "./api/gen/unison/local/v1/local_pb";
import { daemon } from "./daemon.svelte";
import { errorText } from "./errors";
import { translate } from "./i18n";

/** Tabs of the dashboard, in the order they are shown. */
export type DashboardKind = "images" | "files" | "text";

/** Tabs of the dashboard. */
export const DASHBOARD_KINDS: readonly DashboardKind[] = ["images", "files", "text"];

/** Kind of the history that fills one tab. */
export const DASHBOARD_HISTORY_KIND: Record<DashboardKind, Kind> = {
  images: Kind.IMAGE,
  files: Kind.FILES,
  text: Kind.TEXT,
};

/** Numbers of items the user can pick for a tab. */
export const DASHBOARD_LIMIT_OPTIONS = [6, 12, 24, 48] as const;

/** Number of items the user can pick for a tab. */
export type DashboardLimit = (typeof DASHBOARD_LIMIT_OPTIONS)[number];

/** Number of items a tab shows when the user picked none. */
export const DEFAULT_DASHBOARD_LIMIT = 12;

/** STORAGE_KEY keeps the number of items across restarts. */
const STORAGE_KEY = "unison.dashboard.limit";

/** Message for the user after an action of the screen. */
export interface DashboardNotice {
  kind: "info" | "error";
  text: string;
}

/** Items of every tab, newest first. */
export type DashboardItems = Record<DashboardKind, HistoryItem[]>;

/** isLimit reports a number of items the settings offer. */
export function isDashboardLimit(value: number): value is DashboardLimit {
  return (DASHBOARD_LIMIT_OPTIONS as readonly number[]).includes(value);
}

/** dashboardLimitFrom returns the stored number of items, or the default when
 * the value is not offered. */
export function dashboardLimitFrom(value: string | null): DashboardLimit {
  const parsed = Number(value);
  return isDashboardLimit(parsed) ? parsed : DEFAULT_DASHBOARD_LIMIT;
}

/** storedLimit returns the number of items of the last session. */
function storedLimit(): DashboardLimit {
  if (typeof localStorage === "undefined") {
    return DEFAULT_DASHBOARD_LIMIT;
  }
  return dashboardLimitFrom(localStorage.getItem(STORAGE_KEY));
}

/**
 * DashboardStore keeps the newest items of every kind for the dashboard. The
 * lists come from the history of the daemon, so the store follows its events
 * and reloads them, capped by the configured number.
 */
class DashboardStore {
  /** Number of newest items each tab keeps. */
  limit = $state<DashboardLimit>(storedLimit());
  /** Items of every tab, newest first. */
  items = $state<DashboardItems>({ images: [], files: [], text: [] });
  /** Tabs with a visible reload in flight. */
  loading = $state<DashboardKind[]>([]);
  /** Reason of the last failure, absent when it works. */
  error = $state<string | null>(null);
  /** Message of the last action. */
  notice = $state<DashboardNotice | null>(null);
  /** Directory that receives downloaded files. */
  downloadsDir = $state("");

  #started = false;
  #unsubscribe: (() => void) | null = null;
  #inFlight = new Set<DashboardKind>();
  #dirty = new Set<DashboardKind>();

  /** start follows the events of the daemon. */
  start(): void {
    if (this.#started) {
      return;
    }
    this.#started = true;
    this.#unsubscribe = daemon.onEvent((event) => this.#onEvent(event));
    void this.loadSettings();
  }

  /** stop releases the subscription of the store. */
  stop(): void {
    this.#started = false;
    this.#unsubscribe?.();
    this.#unsubscribe = null;
  }

  /** setLimit stores the number of items each tab keeps and reloads them. */
  setLimit(limit: DashboardLimit): void {
    this.limit = limit;
    if (typeof localStorage !== "undefined") {
      localStorage.setItem(STORAGE_KEY, String(limit));
    }
    if (this.#started) {
      void this.loadAll(true);
    }
  }

  /** load reloads one tab. A quiet reload keeps the list on screen. */
  async load(kind: DashboardKind, quiet = false): Promise<void> {
    if (this.#inFlight.has(kind)) {
      // The event that asks for the reload arrived while the list was loading,
      // so it is read again when the answer is there.
      this.#dirty.add(kind);
      return;
    }
    this.#inFlight.add(kind);
    if (!quiet) {
      this.loading = [...this.loading, kind];
    }
    try {
      const response = await client.listHistory({
        kind: DASHBOARD_HISTORY_KIND[kind],
        limit: this.limit,
      });
      this.items = { ...this.items, [kind]: response.items.slice(0, this.limit) };
      this.error = null;
    } catch (error) {
      this.error = errorText(error, translate("dashboard.loadFailed"));
    } finally {
      this.#inFlight.delete(kind);
      this.loading = this.loading.filter((current) => current !== kind);
      if (this.#dirty.delete(kind)) {
        void this.load(kind, true);
      }
    }
  }

  /** loadAll reloads every tab. */
  async loadAll(quiet = false): Promise<void> {
    await Promise.all(DASHBOARD_KINDS.map((kind) => this.load(kind, quiet)));
  }

  /** copy writes an item back to the local clipboard. */
  async copy(id: string): Promise<void> {
    this.notice = null;
    try {
      await client.copyHistoryItem({ id });
      this.notice = { kind: "info", text: translate("history.onClipboard") };
    } catch (error) {
      this.notice = { kind: "error", text: errorText(error, translate("dashboard.copyFailed")) };
    }
  }

  /** download stores the files of an item in the downloads directory. */
  async download(id: string): Promise<void> {
    if (!id) {
      return;
    }
    this.notice = null;
    try {
      const response = await client.fetchHistoryItem({ id, path: this.downloadsDir });
      if (response.files.length > 0) {
        this.notice = {
          kind: "info",
          text: translate("history.saved", { paths: response.files.join(", ") }),
        };
      } else {
        this.notice = { kind: "info", text: translate("history.downloadedLocal") };
      }
      void this.loadAll(true);
    } catch (error) {
      this.notice = {
        kind: "error",
        text: errorText(error, translate("dashboard.downloadFailed")),
      };
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
      case EventKind.CLIP_APPLY:
      case EventKind.PEER_ONLINE:
        void this.loadAll(true);
        break;
    }
  }
}

/** dashboard is the dashboard state of the app. */
export const dashboard = new DashboardStore();
