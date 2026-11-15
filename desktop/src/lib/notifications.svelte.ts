import {
  EventKind,
  TransferDirection,
  TransferState,
  type Event as DaemonEvent,
  type Transfer,
} from "./api/gen/unison/local/v1/local_pb";
import { daemon } from "./daemon.svelte";
import { kindLabel } from "./format";
import { notify } from "./notify";

/** Notifications of the last minutes are remembered to keep repeated events
 * from showing the same message. */
const SEEN_LIMIT = 256;

/**
 * NotificationStore shows the OS notifications of the daemon events: a
 * finished transfer and an item that waits for a download. Messages never
 * carry the content of a clip item.
 */
class NotificationStore {
  #started = false;
  #unsubscribe: (() => void) | null = null;
  #seen = new Set<string>();

  /** start follows the daemon events. */
  start(): void {
    if (this.#started) {
      return;
    }
    this.#started = true;
    this.#unsubscribe = daemon.onEvent((event) => this.#onEvent(event));
  }

  /** stop releases the subscription. */
  stop(): void {
    this.#started = false;
    this.#unsubscribe?.();
    this.#unsubscribe = null;
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
      case EventKind.TRANSFER:
        if (event.transfer) {
          this.#onTransfer(event.transfer);
        }
        break;
    }
  }

  #onTransfer(transfer: Transfer): void {
    const peer = transfer.deviceName || "a device";
    const files = transfer.files.length === 1 ? "1 file" : `${transfer.files.length} files`;
    switch (transfer.state) {
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

  #show(key: string, request: { title: string; body: string }): void {
    if (this.#seen.has(key)) {
      return;
    }
    if (this.#seen.size >= SEEN_LIMIT) {
      this.#seen.clear();
    }
    this.#seen.add(key);
    void notify(request);
  }
}

/** notifications is the notification state of the app. */
export const notifications = new NotificationStore();
