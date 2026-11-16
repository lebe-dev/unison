import { Code, ConnectError } from "@connectrpc/connect";
import { client } from "./api/client";
import {
  EventKind,
  PairingMethod,
  Role,
  type Event as DaemonEvent,
  type PairingCandidate,
  type PairingQr,
  type PairingWindow,
  type Platform,
  type Sas,
} from "./api/gen/unison/local/v1/local_pb";
import { daemon } from "./daemon.svelte";
import { cleanError, errorText } from "./errors";

/** Interval of the candidate list while the pairing window is open. */
const CANDIDATE_INTERVAL = 2_000;

/** State of the open pairing window. */
export interface OpenWindow {
  role: Role;
  addr: string;
  expiresAt: number;
}

/** A device that asks to pair. */
export interface PairingRequest {
  deviceID: string;
  name: string;
  platform: Platform;
  role: Role;
  /** How the exchange was started: a compared code or a scanned square. */
  method: PairingMethod;
  /** Short authentication string of a compared code, absent for a scan. */
  sas: Sas | null;
}

/** Outcome of a pairing exchange. */
export interface PairingResult {
  paired: boolean;
  message: string;
}

/**
 * PairingStore keeps the pairing window and the exchanges of the app. The
 * window and the exchanges live in the daemon, so the store only follows the
 * events and asks the local API for the candidate list.
 */
class PairingStore {
  /** Role the next window proposes to the devices it pairs with. */
  role = $state<Role>(Role.PERSONAL);
  /** Open window, absent when the pairing is off. */
  window = $state<OpenWindow | null>(null);
  /** Square for another device to scan, absent without an open window. */
  qr = $state<PairingQr | null>(null);
  /** Devices that are open for pairing. */
  candidates = $state<PairingCandidate[]>([]);
  /** Device that waits for the comparison of the code. */
  request = $state<PairingRequest | null>(null);
  /** The code of the request was accepted on this device. */
  confirmed = $state(false);
  /** Address of the candidate the exchange was started with. */
  dialing = $state<string | null>(null);
  /** Outcome of the last exchange, absent until it ends. */
  result = $state<PairingResult | null>(null);
  /** Reason of the last failure, absent when the last call succeeded. */
  error = $state<string | null>(null);
  /** A call of the local API is in flight. */
  busy = $state(false);

  #candidateTimer: ReturnType<typeof setInterval> | null = null;
  #expiryTimer: ReturnType<typeof setTimeout> | null = null;
  #unsubscribe: (() => void) | null = null;
  #refreshing = false;
  #started = false;

  /** start follows the events of the daemon. */
  start(): void {
    if (this.#started) {
      return;
    }
    this.#started = true;
    this.#unsubscribe = daemon.onEvent((event) => this.#onEvent(event));
  }

  /** stop releases the subscriptions of the store. */
  stop(): void {
    this.#started = false;
    this.#unsubscribe?.();
    this.#unsubscribe = null;
    this.#stopCandidates();
    this.#clearExpiry();
  }

  /** open opens a pairing window that proposes the selected role. */
  async open(): Promise<void> {
    this.busy = true;
    this.error = null;
    this.result = null;
    try {
      const response = await client.openPairingWindow({ role: this.role });
      this.#applyWindow(response.window, this.role);
      this.request = null;
      this.confirmed = false;
      this.dialing = null;
      await Promise.all([this.refreshCandidates(), this.loadQr()]);
    } catch (error) {
      this.error = errorText(error, "The pairing window could not be opened.");
    } finally {
      this.busy = false;
    }
  }

  /** extend restarts the auto-close timer of the window. */
  async extend(): Promise<void> {
    this.busy = true;
    this.error = null;
    try {
      const response = await client.extendPairingWindow({});
      this.#applyWindow(response.window);
      await this.loadQr();
    } catch (error) {
      this.error = errorText(error, "The pairing window could not be extended.");
      if (error instanceof ConnectError && error.code === Code.FailedPrecondition) {
        this.#closeWindow();
      }
    } finally {
      this.busy = false;
    }
  }

  /** closeWindow closes the window and the exchanges in it. */
  async close(): Promise<void> {
    this.busy = true;
    try {
      await client.closePairingWindow({});
    } catch (error) {
      this.error = errorText(error, "The pairing window could not be closed.");
    } finally {
      this.#closeWindow();
      this.busy = false;
    }
  }

  /** refreshCandidates reloads the devices that are open for pairing. */
  async refreshCandidates(): Promise<void> {
    if (!this.window || this.#refreshing) {
      return;
    }
    this.#refreshing = true;
    try {
      const response = await client.listPairingCandidates({});
      this.candidates = response.candidates;
    } catch (error) {
      if (!(error instanceof ConnectError && error.code === Code.Unavailable)) {
        this.error = errorText(error, "The candidate list could not be loaded.");
      }
    } finally {
      this.#refreshing = false;
    }
  }

  /** loadQr reloads the square of the window. A completed pairing spends the
   * code, so the daemon answers with a new one and the screen has to follow. */
  async loadQr(): Promise<void> {
    if (!this.window) {
      return;
    }
    try {
      const response = await client.getPairingQr({});
      this.qr = response.qr ?? null;
    } catch (error) {
      if (!(error instanceof ConnectError && error.code === Code.Unavailable)) {
        this.error = errorText(error, "The pairing code could not be loaded.");
      }
    }
  }

  /** pairWith starts an exchange with a discovered candidate. */
  async pairWith(addr: string): Promise<void> {
    this.error = null;
    this.result = null;
    this.dialing = addr;
    try {
      await client.startSas({ addr });
    } catch (error) {
      this.dialing = null;
      this.error = errorText(error, "The pairing exchange could not be started.");
    }
  }

  /** confirm accepts the code of the request. */
  async confirm(): Promise<void> {
    const request = this.request;
    if (!request) {
      return;
    }
    this.busy = true;
    this.error = null;
    try {
      await client.confirmSas({ deviceId: request.deviceID });
      this.confirmed = true;
    } catch (error) {
      this.error = errorText(error, "The code could not be confirmed.");
    } finally {
      this.busy = false;
    }
  }

  /** reject refuses the request. */
  async reject(): Promise<void> {
    const request = this.request;
    if (!request) {
      return;
    }
    this.busy = true;
    this.error = null;
    try {
      await client.rejectSas({ deviceId: request.deviceID });
      this.request = null;
      this.result = { paired: false, message: "Pairing was rejected." };
    } catch (error) {
      this.error = errorText(error, "The request could not be rejected.");
    } finally {
      this.busy = false;
      this.confirmed = false;
    }
  }

  #onEvent(event: DaemonEvent): void {
    switch (event.kind) {
      case EventKind.PAIRING_CANDIDATE:
        if (this.window) {
          void this.refreshCandidates();
        }
        break;
      case EventKind.PAIRING_REQUEST:
        this.request = {
          deviceID: event.deviceId,
          name: event.name,
          platform: event.platform,
          role: event.role,
          method: event.pairingMethod,
          sas: event.sas ?? null,
        };
        this.confirmed = false;
        this.dialing = null;
        this.result = null;
        break;
      case EventKind.PAIRING_PAIRED:
        this.request = null;
        this.confirmed = false;
        this.dialing = null;
        this.result = {
          paired: true,
          message: event.name ? `${event.name} is paired.` : "The device is paired.",
        };
        void daemon.refresh();
        void this.refreshCandidates();
        void this.loadQr();
        break;
      case EventKind.PAIRING_FAILED:
        this.request = null;
        this.confirmed = false;
        this.dialing = null;
        this.result = {
          paired: false,
          message: cleanError(event.error) || "The pairing exchange failed.",
        };
        break;
      case EventKind.PAIRING_CLOSED:
        this.request = null;
        this.confirmed = false;
        this.dialing = null;
        this.#closeWindow();
        break;
    }
  }

  /** #applyWindow stores the window of a response and follows it. */
  #applyWindow(window: PairingWindow | undefined, role?: Role): void {
    if (!window?.open) {
      this.#closeWindow();
      return;
    }
    const expiresAt = Number(window.expiresAtUnixMs);
    this.window = {
      role: role ?? this.window?.role ?? this.role,
      addr: window.addr,
      expiresAt,
    };
    this.#startCandidates();
    this.#scheduleExpiry(expiresAt);
  }

  /** #closeWindow drops the window state and the polling. */
  #closeWindow(): void {
    this.window = null;
    this.qr = null;
    this.candidates = [];
    this.#stopCandidates();
    this.#clearExpiry();
  }

  #startCandidates(): void {
    if (this.#candidateTimer) {
      return;
    }
    this.#candidateTimer = setInterval(() => void this.refreshCandidates(), CANDIDATE_INTERVAL);
  }

  #stopCandidates(): void {
    if (!this.#candidateTimer) {
      return;
    }
    clearInterval(this.#candidateTimer);
    this.#candidateTimer = null;
  }

  /** #scheduleExpiry closes the window when its deadline passes. The daemon
   * sends the closing event as well, the timer keeps the screen in step. */
  #scheduleExpiry(expiresAt: number): void {
    this.#clearExpiry();
    const delay = Math.max(0, expiresAt - Date.now()) + 250;
    this.#expiryTimer = setTimeout(() => {
      if (this.window && this.window.expiresAt <= Date.now() + 250) {
        this.request = null;
        this.confirmed = false;
        this.dialing = null;
        this.#closeWindow();
      }
    }, delay);
  }

  #clearExpiry(): void {
    if (!this.#expiryTimer) {
      return;
    }
    clearTimeout(this.#expiryTimer);
    this.#expiryTimer = null;
  }
}

/** pairing is the pairing state of the app. */
export const pairing = new PairingStore();
