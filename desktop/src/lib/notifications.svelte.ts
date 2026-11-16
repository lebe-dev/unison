import type { UnlistenFn } from "@tauri-apps/api/event";
import {
  EventKind,
  TransferDirection,
  TransferState,
  type Event as DaemonEvent,
  type Transfer,
} from "./api/gen/unison/local/v1/local_pb";
import { daemon } from "./daemon.svelte";
import { formatBytes, kindLabel, transferFilesText } from "./format";
import { history } from "./history.svelte";
import {
  notify,
  onNotificationAction,
  type NotificationAction,
  type NotificationRequest,
  type PerformedAction,
} from "./notify";
import { requests } from "./requests.svelte";

/** Notifications of the last minutes are remembered to keep repeated events
 * from showing the same message. */
const SEEN_LIMIT = 256;

/** Buttons of the notification of a Friends request. */
const REQUEST_ACTIONS: NotificationAction[] = [
  { id: "accept", title: "Accept" },
  { id: "decline", title: "Decline" },
];

/** Button of the notification of an item that waits for confirmation. */
const APPLY_ACTIONS: NotificationAction[] = [{ id: "apply", title: "Apply" }];

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
          title: "Item available for download",
          body: `${kindLabel(event.item.kind)} from ${event.name || "a device"} waits in the history.`,
        });
        break;
      case EventKind.CLIP_APPLY:
        if (!event.item) {
          return;
        }
        this.#show(`hold:${event.item.id}`, {
          title: `Clipboard from ${event.name || "a device"}`,
          body: `${kindLabel(event.item.kind)} - apply it when you need it.`,
          actions: APPLY_ACTIONS,
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
    }
  }

  #onTransfer(transfer: Transfer): void {
    const peer = transfer.deviceName || "a device";
    const files = transfer.files.length === 1 ? "1 file" : `${transfer.files.length} files`;
    switch (transfer.state) {
      case TransferState.PENDING:
        if (transfer.direction !== TransferDirection.INCOMING) {
          return;
        }
        this.#show(`request:${transfer.id}`, {
          title: `Files from ${peer}`,
          body: `${transferFilesText(transfer)} (${formatBytes(transfer.totalBytes)}) waits for your answer.`,
          actions: REQUEST_ACTIONS,
          extra: { id: transfer.id },
        });
        break;
      case TransferState.DONE:
        this.#show(`transfer:${transfer.id}:done`, {
          title:
            transfer.direction === TransferDirection.INCOMING ? "Files received" : "Files sent",
          body:
            transfer.direction === TransferDirection.INCOMING
              ? `${files} from ${peer} is saved.`
              : `${files} is sent to ${peer}.`,
        });
        break;
      case TransferState.FAILED:
        this.#show(`transfer:${transfer.id}:failed`, {
          title: "Transfer failed",
          body: `${files} with ${peer}: ${transfer.error || "the transfer stopped."}`,
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

/** notifications is the notification state of the app. */
export const notifications = new NotificationStore();
