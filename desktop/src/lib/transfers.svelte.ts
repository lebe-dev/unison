import { client } from "./api/client";
import {
  EventKind,
  TransferState,
  type Event as DaemonEvent,
  type Transfer,
} from "./api/gen/unison/local/v1/local_pb";
import { daemon } from "./daemon.svelte";
import { errorText } from "./errors";

/** Message for the user after an action of the screen. */
export interface TransferNotice {
  kind: "info" | "error";
  text: string;
}

/** States of a transfer that still runs. */
const ACTIVE_STATES: TransferState[] = [
  TransferState.PENDING,
  TransferState.OFFERED,
  TransferState.TRANSFERRING,
];

/** isActive reports a transfer that has not reached a final state. */
export function isActiveTransfer(transfer: Transfer): boolean {
  return ACTIVE_STATES.includes(transfer.state);
}

/**
 * TransferStore keeps the file transfers of the daemon. Every stage and every
 * progress report arrives as an event, so the store replaces the record of the
 * event in the list instead of reloading it.
 */
class TransferStore {
  /** Transfers of this device, newest first. */
  transfers = $state<Transfer[]>([]);
  /** A reload of the list is in flight. */
  loading = $state(false);
  /** Reason of the last failure of the list, absent when it works. */
  error = $state<string | null>(null);
  /** Message of the last action. */
  notice = $state<TransferNotice | null>(null);

  #started = false;
  #unsubscribe: (() => void) | null = null;

  get active(): Transfer[] {
    return this.transfers.filter(isActiveTransfer);
  }

  get finished(): Transfer[] {
    return this.transfers.filter((transfer) => !isActiveTransfer(transfer));
  }

  /** start follows the events of the daemon. */
  start(): void {
    if (this.#started) {
      return;
    }
    this.#started = true;
    this.#unsubscribe = daemon.onEvent((event) => this.#onEvent(event));
    void this.refresh();
  }

  /** stop releases the subscriptions of the store. */
  stop(): void {
    this.#started = false;
    this.#unsubscribe?.();
    this.#unsubscribe = null;
  }

  /** refresh reloads the list from the daemon. */
  async refresh(): Promise<void> {
    this.loading = true;
    try {
      const response = await client.listTransfers({});
      this.transfers = sortTransfers(response.transfers);
      this.error = null;
    } catch (error) {
      this.error = errorText(error, "The transfers could not be loaded.");
    } finally {
      this.loading = false;
    }
  }

  /** send offers files to the given devices. */
  async send(paths: string[], deviceIds: string[]): Promise<void> {
    if (paths.length === 0) {
      return;
    }
    this.notice = null;
    try {
      const response = await client.sendFiles({ paths, deviceIds });
      for (const transfer of response.transfers) {
        this.#upsert(transfer);
      }
      const count = response.transfers.length;
      this.notice = {
        kind: "info",
        text: count === 1 ? "The offer is sent." : `${count} offers are sent.`,
      };
    } catch (error) {
      this.notice = { kind: "error", text: errorText(error, "The files could not be sent.") };
    }
  }

  /** cancel stops a transfer. */
  async cancel(id: string): Promise<void> {
    this.notice = null;
    try {
      await client.cancelTransfer({ id });
    } catch (error) {
      this.notice = { kind: "error", text: errorText(error, "The transfer could not be canceled.") };
    }
  }

  /** accept takes an incoming offer. */
  async accept(id: string): Promise<void> {
    this.notice = null;
    try {
      await client.acceptTransfer({ id });
    } catch (error) {
      this.notice = { kind: "error", text: errorText(error, "The offer could not be accepted.") };
    }
  }

  /** reject declines an incoming offer. */
  async reject(id: string): Promise<void> {
    this.notice = null;
    try {
      await client.rejectTransfer({ id });
    } catch (error) {
      this.notice = { kind: "error", text: errorText(error, "The offer could not be declined.") };
    }
  }

  #onEvent(event: DaemonEvent): void {
    switch (event.kind) {
      case EventKind.TRANSFER:
        if (event.transfer) {
          this.#upsert(event.transfer);
        }
        break;
      case EventKind.PEER_ONLINE:
        void this.refresh();
        break;
    }
  }

  /** #upsert stores the newest record of one transfer. */
  #upsert(transfer: Transfer): void {
    const known = this.transfers.some((current) => current.id === transfer.id);
    if (known) {
      this.transfers = this.transfers.map((current) =>
        current.id === transfer.id ? transfer : current,
      );
      return;
    }
    this.transfers = sortTransfers([transfer, ...this.transfers]);
  }
}

/** sortTransfers orders the transfers from the newest to the oldest. */
function sortTransfers(transfers: Transfer[]): Transfer[] {
  return [...transfers].sort(
    (left, right) => Number(right.createdAtUnixMs - left.createdAtUnixMs),
  );
}

/** transfers is the transfer state of the app. */
export const transfers = new TransferStore();
