import { client } from "./api/client";
import {
  EventKind,
  MediatedPairDirection,
  MediatedPairState,
  MediatedPairCancelReason,
  Role,
  type Event as DaemonEvent,
  type MediatedPair,
} from "./api/gen/unison/local/v1/local_pb";
import { daemon } from "./daemon.svelte";
import { errorText } from "./errors";
import { translate, type MessageKey } from "./i18n";

/** Refresh of the list, which covers the exchanges the daemon expires on its
 * own. */
const REFRESH_INTERVAL = 20_000;

/** Message for the user after an action of a screen. */
export interface MediatedNotice {
  kind: "info" | "error";
  text: string;
}

/** Labels of the reasons an exchange ended without a new pair. */
const REASON_LABELS: Record<number, MessageKey> = {
  [MediatedPairCancelReason.USER_REJECTED]: "mediated.reason.userRejected",
  [MediatedPairCancelReason.MEDIATOR_CANCELED]: "mediated.reason.mediatorCanceled",
  [MediatedPairCancelReason.EXPIRED]: "mediated.reason.expired",
  [MediatedPairCancelReason.REVOKED]: "mediated.reason.revoked",
  [MediatedPairCancelReason.ROLE_MISMATCH]: "mediated.reason.roleMismatch",
  [MediatedPairCancelReason.ALREADY_PAIRED]: "mediated.reason.alreadyPaired",
  [MediatedPairCancelReason.BUSY]: "mediated.reason.busy",
  [MediatedPairCancelReason.PEER_OFFLINE]: "mediated.reason.peerOffline",
  [MediatedPairCancelReason.FAILED]: "mediated.reason.failed",
};

/**
 * MediatedStore keeps the introductions through a trusted device. The exchanges
 * live in the daemon, which gives them a short deadline, so the store follows
 * the events and reloads the list from time to time.
 */
class MediatedStore {
  /** Exchanges the daemon still holds, from the list and the events. */
  pairs = $state<MediatedPair[]>([]);
  /** A reload of the list is in flight. */
  loading = $state(false);
  /** Reason of the last failure of the list, absent when it works. */
  error = $state<string | null>(null);
  /** Message of the last action. */
  notice = $state<MediatedNotice | null>(null);
  /** Exchanges with an answer in flight. */
  answering = $state<string[]>([]);

  #started = false;
  #unsubscribe: (() => void) | null = null;
  #timer: ReturnType<typeof setInterval> | null = null;
  #loading = false;

  /** incoming lists the offers another device makes to this one, which wait
   * for the answer of the user. */
  get incoming(): MediatedPair[] {
    return this.pairs.filter(
      (pair) =>
        pair.direction === MediatedPairDirection.INCOMING &&
        pair.state === MediatedPairState.WAITING,
    );
  }

  /** outgoing lists the exchanges this device runs for two other devices. */
  get outgoing(): MediatedPair[] {
    return this.pairs.filter((pair) => pair.direction === MediatedPairDirection.MEDIATOR);
  }

  /** pending is the number of offers that wait for an answer. */
  get pending(): number {
    return this.incoming.length;
  }

  /** start follows the events of the daemon. */
  start(): void {
    if (this.#started) {
      return;
    }
    this.#started = true;
    this.#unsubscribe = daemon.onEvent((event) => this.#onEvent(event));
    this.#timer = setInterval(() => void this.load(true), REFRESH_INTERVAL);
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

  /** clearNotice drops the message of the last action. */
  clearNotice(): void {
    this.notice = null;
  }

  /** load reloads the list. A quiet reload keeps the list on screen while the
   * request is in flight. */
  async load(quiet = false): Promise<void> {
    if (this.#loading) {
      return;
    }
    this.#loading = true;
    if (!quiet) {
      this.loading = true;
    }
    try {
      const response = await client.listMediatedPairs({});
      this.pairs = response.pairs;
      this.error = null;
    } catch (error) {
      this.error = errorText(error, translate("mediated.loadFailed"));
    } finally {
      this.#loading = false;
      this.loading = false;
    }
  }

  /** startPair asks the daemon to introduce two of its devices. The session ID
   * is absent when the daemon refused the exchange. */
  async startPair(peerA: string, peerB: string, role: Role): Promise<string | null> {
    this.notice = null;
    try {
      const response = await client.startMediatedPair({ peerA, peerB, role });
      const pair = response.pair;
      if (!pair?.sessionId) {
        return null;
      }
      this.#replace(pair);
      return pair.sessionId;
    } catch (error) {
      this.notice = {
        kind: "error",
        text: errorText(error, translate("mediated.startFailed")),
      };
      return null;
    }
  }

  /** confirm accepts an incoming offer. enableFriendsDiscovery turns the search
   * for Friends devices on when the pair completes. */
  async confirm(sessionId: string, enableFriendsDiscovery: boolean): Promise<void> {
    if (this.answering.includes(sessionId)) {
      return;
    }
    this.answering = [...this.answering, sessionId];
    this.notice = null;
    try {
      const response = await client.confirmMediatedPair({ sessionId, enableFriendsDiscovery });
      this.#settle(sessionId, response.pair);
      this.notice = { kind: "info", text: translate("mediated.confirmed") };
    } catch (error) {
      this.notice = {
        kind: "error",
        text: errorText(error, translate("mediated.confirmFailed")),
      };
    } finally {
      this.answering = this.answering.filter((current) => current !== sessionId);
    }
  }

  /** reject declines an incoming offer. */
  async reject(sessionId: string): Promise<void> {
    if (this.answering.includes(sessionId)) {
      return;
    }
    this.answering = [...this.answering, sessionId];
    this.notice = null;
    try {
      await client.rejectMediatedPair({ sessionId });
      this.#drop(sessionId);
      this.notice = { kind: "info", text: translate("mediated.rejected") };
    } catch (error) {
      this.notice = {
        kind: "error",
        text: errorText(error, translate("mediated.rejectFailed")),
      };
    } finally {
      this.answering = this.answering.filter((current) => current !== sessionId);
    }
  }

  /** cancel ends an exchange this device runs or takes part in. */
  async cancel(sessionId: string): Promise<void> {
    if (this.answering.includes(sessionId)) {
      return;
    }
    this.answering = [...this.answering, sessionId];
    this.notice = null;
    try {
      await client.cancelMediatedPair({ sessionId });
      this.#drop(sessionId);
      this.notice = { kind: "info", text: translate("mediated.canceled") };
    } catch (error) {
      this.notice = {
        kind: "error",
        text: errorText(error, translate("mediated.cancelFailed")),
      };
    } finally {
      this.answering = this.answering.filter((current) => current !== sessionId);
    }
  }

  #onEvent(event: DaemonEvent): void {
    const pair = event.mediatedPair;
    if (!pair) {
      return;
    }
    switch (event.kind) {
      case EventKind.MEDIATED_PAIR_OFFER:
      case EventKind.MEDIATED_PAIR_UPDATED:
        this.#replace(pair);
        return;
      case EventKind.MEDIATED_PAIR_COMPLETED:
        this.#drop(pair.sessionId);
        this.notice = { kind: "info", text: completedText(pair) };
        return;
      case EventKind.MEDIATED_PAIR_CANCELED:
        this.#drop(pair.sessionId);
        // A cancellation the user asked for is reported by the call itself.
        if (
          pair.cancelReason !== MediatedPairCancelReason.USER_REJECTED &&
          pair.cancelReason !== MediatedPairCancelReason.MEDIATOR_CANCELED
        ) {
          this.notice = {
            kind: "error",
            text: translate("mediated.canceledReason", { reason: reasonLabel(pair.cancelReason) }),
          };
        }
        return;
    }
  }

  /** #replace inserts an exchange or updates the one with its session ID. */
  #replace(pair: MediatedPair): void {
    if (!this.pairs.some((current) => current.sessionId === pair.sessionId)) {
      this.pairs = [...this.pairs, pair];
      return;
    }
    this.pairs = this.pairs.map((current) =>
      current.sessionId === pair.sessionId ? pair : current,
    );
  }

  /** #settle applies the pair a call returned, or drops it when the daemon no
   * longer holds the exchange. */
  #settle(sessionId: string, pair: MediatedPair | undefined): void {
    if (
      !pair ||
      !pair.state ||
      pair.state === MediatedPairState.COMPLETED ||
      pair.state === MediatedPairState.CANCELED
    ) {
      this.#drop(sessionId);
      return;
    }
    this.#replace(pair);
  }

  /** #drop removes an exchange that ended. */
  #drop(sessionId: string): void {
    this.pairs = this.pairs.filter((pair) => pair.sessionId !== sessionId);
  }
}

/** completedText names the device an introduction added. */
function completedText(pair: MediatedPair): string {
  if (pair.direction === MediatedPairDirection.INCOMING) {
    const name = pair.peer?.name || translate("common.aDevice");
    return translate("mediated.paired", { name });
  }
  return translate("mediated.completed");
}

/** reasonLabel names why an exchange ended. */
function reasonLabel(reason: MediatedPairCancelReason): string {
  return translate(REASON_LABELS[reason] ?? "mediated.reason.failed");
}

/** mediated is the introduction state of the app. */
export const mediated = new MediatedStore();
