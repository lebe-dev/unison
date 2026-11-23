import { client } from "./api/client";
import {
  EventKind,
  TransferState,
  type Event as DaemonEvent,
  type Transfer,
} from "./api/gen/unison/local/v1/local_pb";
import { daemon } from "./daemon.svelte";
import { errorText } from "./errors";
import { translate } from "./i18n";

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
  /** Files of the offer the user prepares. */
  picked = $state<string[]>([]);
  /** Devices of the offer the user prepares, device IDs. */
  targets = $state<string[]>([]);

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
      this.error = errorText(error, translate("transfers.loadFailed"));
    } finally {
      this.loading = false;
    }
  }

  /**
   * offer starts a new offer of the files to every online device. The files
   * come from the file dialog or from another app that shares them.
   */
  offer(paths: string[]): void {
    if (paths.length === 0) {
      return;
    }
    this.picked = paths;
    this.targets = daemon.devices
      .filter((device) => device.online && !device.revoked)
      .map((device) => device.deviceId);
  }

  /** toggleTarget adds a device to the offer or takes it out. */
  toggleTarget(id: string): void {
    this.targets = this.targets.includes(id)
      ? this.targets.filter((candidate) => candidate !== id)
      : [...this.targets, id];
  }

  /** clearDraft drops the offer the user prepares. */
  clearDraft(): void {
    this.picked = [];
    this.targets = [];
  }

  /** sendDraft sends the prepared offer and drops it once the daemon took it. */
  async sendDraft(): Promise<void> {
    if (await this.send(this.picked, this.targets)) {
      this.clearDraft();
    }
  }

  /** send offers files to the given devices and reports whether the daemon
   * took the offer. */
  async send(paths: string[], deviceIds: string[]): Promise<boolean> {
    if (paths.length === 0) {
      return false;
    }
    this.notice = null;
    try {
      const response = await client.sendFiles({ paths, deviceIds });
      for (const transfer of response.transfers) {
        this.#upsert(transfer);
      }
      this.notice = {
        kind: "info",
        text: translate("transfers.offersSent", { count: response.transfers.length }),
      };
      return true;
    } catch (error) {
      this.notice = {
        kind: "error",
        text: errorText(error, translate("transfers.sendFailed")),
      };
      return false;
    }
  }

  /** cancel stops a transfer. */
  async cancel(id: string): Promise<void> {
    this.notice = null;
    try {
      await client.cancelTransfer({ id });
    } catch (error) {
      this.notice = {
        kind: "error",
        text: errorText(error, translate("transfers.cancelFailed")),
      };
    }
  }

  /** accept takes an incoming offer. */
  async accept(id: string): Promise<void> {
    this.notice = null;
    try {
      await client.acceptTransfer({ id });
    } catch (error) {
      this.notice = {
        kind: "error",
        text: errorText(error, translate("transfers.acceptFailed")),
      };
    }
  }

  /** reject declines an incoming offer. */
  async reject(id: string): Promise<void> {
    this.notice = null;
    try {
      await client.rejectTransfer({ id });
    } catch (error) {
      this.notice = {
        kind: "error",
        text: errorText(error, translate("transfers.rejectFailed")),
      };
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
  return [...transfers].sort((left, right) => Number(right.createdAtUnixMs - left.createdAtUnixMs));
}

/** transfers is the transfer state of the app. */
export const transfers = new TransferStore();
