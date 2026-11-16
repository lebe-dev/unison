import { client } from "./api/client";
import {
  EventKind,
  TransferState,
  type Event as DaemonEvent,
  type ShareRequest,
} from "./api/gen/unison/local/v1/local_pb";
import { daemon } from "./daemon.svelte";
import { errorText } from "./errors";

/** Refresh of the list, which covers the requests the daemon expires on its
 * own. */
const REFRESH_INTERVAL = 20_000;

/** Message for the user after an action of the screen. */
export interface RequestNotice {
  kind: "info" | "error";
  text: string;
}

/**
 * RequestStore keeps the file offers of Friends that wait for an answer. The
 * daemon holds every offer in memory until the user accepts or declines it or
 * the offer expires, so the store follows the events and reloads the list.
 */
class RequestStore {
  /** Offers that wait for an answer, newest first. */
  requests = $state<ShareRequest[]>([]);
  /** A reload of the list is in flight. */
  loading = $state(false);
  /** Reason of the last failure of the list, absent when it works. */
  error = $state<string | null>(null);
  /** Message of the last action. */
  notice = $state<RequestNotice | null>(null);
  /** Offers with an answer in flight. */
  answering = $state<string[]>([]);

  #started = false;
  #unsubscribe: (() => void) | null = null;
  #timer: ReturnType<typeof setInterval> | null = null;
  #loading = false;

  /** pending is the number of offers that wait for an answer. */
  get pending(): number {
    return this.requests.length;
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
      const response = await client.listRequests({});
      this.requests = response.requests;
      this.error = null;
    } catch (error) {
      this.error = errorText(error, "The requests could not be loaded.");
    } finally {
      this.#loading = false;
      this.loading = false;
    }
  }

  /** decide accepts or declines an offer and tells the sender. */
  async decide(id: string, accept: boolean): Promise<void> {
    if (this.answering.includes(id)) {
      return;
    }
    this.answering = [...this.answering, id];
    this.notice = null;
    try {
      await client.decideRequest({ id, accept });
      this.requests = this.requests.filter((request) => request.id !== id);
      this.notice = {
        kind: "info",
        text: accept ? "The files are on their way." : "The request is declined.",
      };
    } catch (error) {
      this.notice = {
        kind: "error",
        text: errorText(error, "The request could not be answered."),
      };
    } finally {
      this.answering = this.answering.filter((current) => current !== id);
    }
  }

  #onEvent(event: DaemonEvent): void {
    if (event.kind !== EventKind.TRANSFER || !event.transfer) {
      return;
    }
    const transfer = event.transfer;
    if (transfer.state === TransferState.PENDING) {
      void this.load(true);
      return;
    }
    // An offered request that ended is no longer on the list.
    this.requests = this.requests.filter((request) => request.id !== transfer.id);
  }
}

/** requests is the incoming request state of the app. */
export const requests = new RequestStore();
