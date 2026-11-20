import type { UnlistenFn } from "@tauri-apps/api/event";
import {
  EventKind,
  TransferDirection,
  TransferState,
  type Event as DaemonEvent,
  type Transfer,
} from "./api/gen/unison/local/v1/local_pb";
import { daemon } from "./daemon.svelte";
import { openFiles, showInFolder } from "./files";
import { filesCountText, formatBytes, kindLabel, transferFilesText } from "./format";
import { history } from "./history.svelte";
import { translate } from "./i18n";
import {
  notify,
  onNotificationAction,
  type NotificationAction,
  type NotificationRequest,
  type PerformedAction,
} from "./notify";
import { requests } from "./requests.svelte";
import { showWindow } from "./window";

/** Notifications of the last minutes are remembered to keep repeated events
 * from showing the same message. */
const SEEN_LIMIT = 256;

/** Buttons of the notification of a Friends request. */
function requestActions(): NotificationAction[] {
  return [
    { id: "accept", title: translate("common.accept") },
    { id: "decline", title: translate("common.decline") },
  ];
}

/** Button of the notification of an item that waits for confirmation. */
function applyActions(): NotificationAction[] {
  return [{ id: "apply", title: translate("common.apply") }];
}

/** Buttons of the notification of the received files. */
function receivedActions(): NotificationAction[] {
  return [
    { id: "show-in-folder", title: translate("notifications.showInFolder") },
    { id: "open", title: translate("common.open") },
  ];
}

/**
 * NotificationStore shows the OS notifications of the daemon events: an
 * incoming request of Friends with the answer buttons, an item that waits for
 * confirmation, a finished transfer and an item that waits for a download.
 * Messages never carry the content of a clip item.
 */
class NotificationStore {
  #started = false;
  #unsubscribe: (() => void) | null = null;
  #unlisten: UnlistenFn | null = null;
  #seen = new Set<string>();

  /** start follows the daemon events and the actions of the notifications. */
  async start(): Promise<void> {
    if (this.#started) {
      return;
    }
    this.#started = true;
    this.#unsubscribe = daemon.onEvent((event) => this.#onEvent(event));
    const unlisten = await onNotificationAction((action) => this.#onAction(action)).catch(
      (error: unknown) => {
        console.warn("cannot subscribe to the notification actions", error);
        return null;
      },
    );
    if (this.#started) {
      this.#unlisten = unlisten;
      return;
    }
    // The shell stopped while the subscription was on its way.
    unlisten?.();
  }

  /** stop releases the subscriptions. */
  stop(): void {
    this.#started = false;
    this.#unsubscribe?.();
    this.#unsubscribe = null;
    this.#unlisten?.();
    this.#unlisten = null;
    this.#seen.clear();
  }

  #onEvent(event: DaemonEvent): void {
    switch (event.kind) {
      case EventKind.CLIP_DOWNLOAD:
        if (!event.item) {
          return;
        }
        this.#show(`clip:${event.item.id}`, {
          title: translate("notifications.itemAvailableTitle"),
          body: translate("notifications.itemAvailableBody", {
            kind: kindLabel(event.item.kind),
            device: event.name || translate("common.aDeviceLower"),
          }),
        });
        break;
      case EventKind.CLIP_APPLY:
        if (!event.item) {
          return;
        }
        this.#show(`hold:${event.item.id}`, {
          title: translate("notifications.clipboardFrom", {
            device: event.name || translate("common.aDeviceLower"),
          }),
          body: translate("notifications.clipboardBody", {
            kind: kindLabel(event.item.kind),
          }),
          actions: applyActions(),
          extra: { id: event.item.id },
        });
        break;
      case EventKind.TRANSFER:
        if (event.transfer) {
          this.#onTransfer(event.transfer);
        }
        break;
    }
  }

  #onAction(action: PerformedAction): void {
    const id = extraID(action.extra);
    switch (action.actionId) {
      // A desktop may show no buttons for the notification, so the click on the
      // notification itself brings the window with the waiting request back.
      case "tap":
      case "default":
        void showWindow().catch((error: unknown) => {
          console.warn("cannot show the window", error);
        });
        break;
      case "accept":
        if (id) {
          void requests.decide(id, true);
        }
        break;
      case "decline":
        if (id) {
          void requests.decide(id, false);
        }
        break;
      case "apply":
        if (id) {
          void history.apply(id);
        }
        break;
      case "show-in-folder":
        this.#handFiles(action, showInFolder);
        break;
      case "open":
        this.#handFiles(action, openFiles);
        break;
    }
  }

  /** #handFiles hands the files of the notification to the shell. */
  #handFiles(action: PerformedAction, run: (paths: string[]) => Promise<void>): void {
    const paths = extraPaths(action.extra);
    if (paths.length === 0) {
      return;
    }
    void run(paths).catch((error: unknown) => {
      console.warn("the files of the notification cannot be opened", error);
    });
  }

  #onTransfer(transfer: Transfer): void {
    const count = transfer.files.length;
    const peer = transfer.deviceName || translate("common.aDeviceLower");
    const files = filesCountText(count);
    switch (transfer.state) {
      case TransferState.PENDING:
        if (transfer.direction !== TransferDirection.INCOMING) {
          return;
        }
        this.#show(`request:${transfer.id}`, {
          title: translate("notifications.filesFrom", { device: peer }),
          body: translate("notifications.waitingBody", {
            count,
            files: transferFilesText(transfer),
            size: formatBytes(transfer.totalBytes),
          }),
          actions: requestActions(),
          extra: { id: transfer.id },
        });
        break;
      case TransferState.DONE: {
        const incoming = transfer.direction === TransferDirection.INCOMING;
        const received = incoming && transfer.paths.length > 0;
        this.#show(`transfer:${transfer.id}:done`, {
          title: translate(incoming ? "notifications.receivedTitle" : "notifications.sentTitle"),
          body: translate(incoming ? "notifications.receivedBody" : "notifications.sentBody", {
            count,
            files,
            peer,
          }),
          ...(received ? { actions: receivedActions(), extra: { paths: transfer.paths } } : {}),
        });
        break;
      }
      case TransferState.FAILED:
        this.#show(`transfer:${transfer.id}:failed`, {
          title: translate("notifications.failedTitle"),
          body: translate("notifications.failedBody", {
            count,
            files,
            peer,
            error: transfer.error || translate("notifications.transferStopped"),
          }),
        });
        break;
    }
  }

  #show(key: string, request: Omit<NotificationRequest, "key">): void {
    if (this.#seen.has(key)) {
      return;
    }
    if (this.#seen.size >= SEEN_LIMIT) {
      this.#seen.clear();
    }
    this.#seen.add(key);
    void notify({ ...request, key });
  }
}

/** extraID reads the identity of the subject of a notification. */
function extraID(extra: Record<string, unknown>): string {
  const value = extra.id;
  return typeof value === "string" ? value : "";
}

/** extraPaths reads the files of the subject of a notification. */
function extraPaths(extra: Record<string, unknown>): string[] {
  const value = extra.paths;
  if (!Array.isArray(value)) {
    return [];
  }
  return value.filter((path): path is string => typeof path === "string" && path !== "");
}

/** notifications is the notification state of the app. */
export const notifications = new NotificationStore();
