import { ConnectError } from "@connectrpc/connect";
import { client } from "./api/client";
import {
  EventKind,
  PauseMode,
  type Device,
  type Event as DaemonEvent,
  type GetStatusResponse,
} from "./api/gen/unison/local/v1/local_pb";
import { updateTray, type PauseChoice } from "./tray";
import { daemonStatus, type DaemonStatus } from "./daemonStatus";
import { shortDeviceID } from "./format";

/** Times of the shell. */
const STATUS_INTERVAL = 5_000;
const RETRY_DELAY = 3_000;
const ACTIVITY_LIMIT = 5;
const FIVE_MINUTES = 5 * 60_000;
const ONE_HOUR = 60 * 60_000;

/**
 * DaemonStore keeps the state of the daemon for the shell. The status is
 * polled, because a pause changed by the tray or by a terminal does not
 * produce an event.
 */
class DaemonStore {
  /** Last status of the daemon, absent when it does not answer. */
  status = $state<GetStatusResponse | null>(null);
  /** Reason of the last failure, absent when the daemon answers. */
  error = $state<string | null>(null);
  /** State of the daemon as the app started it. */
  startup = $state<DaemonStatus>({ state: "offline", reason: null });
  /** Most recent daemon events, newest first. */
  activity = $state<DaemonEvent[]>([]);

  #running = false;
  #statusTimer: ReturnType<typeof setInterval> | null = null;
  #events = new Set<(event: DaemonEvent) => void>();
  #stream: AbortController | null = null;

  get connected(): boolean {
    return this.status?.running ?? false;
  }

  /** loading reports a first status that has not arrived yet. */
  get loading(): boolean {
    return this.status === null && this.error === null;
  }

  /** failed reports a daemon that the app could not start. */
  get failed(): boolean {
    return !this.connected && this.startup.state === "failed";
  }

  /** offlineReason explains why the daemon does not answer, absent when it
   * runs or when the app knows no reason. */
  get offlineReason(): string | null {
    return this.failed ? this.startup.reason : null;
  }

  get deviceName(): string {
    return this.status?.deviceName ?? "Unison";
  }

  get deviceID(): string {
    return this.status?.deviceId ?? "";
  }

  get devices(): Device[] {
    return this.status?.devices ?? [];
  }

  get onlinePeers(): number {
    return this.devices.filter((device) => device.online).length;
  }

  get paused(): boolean {
    return this.status?.pause?.paused ?? false;
  }

  get pauseText(): string {
    if (!this.connected) {
      return "Daemon offline";
    }
    const pause = this.status?.pause;
    if (!pause?.paused) {
      return "Clipboard active";
    }
    if (pause.indefinite) {
      return "Clipboard paused";
    }
    const until = new Date(Number(pause.untilUnixMs)).toLocaleTimeString();
    return `Clipboard paused until ${until}`;
  }

  /** start loads the status and follows the events of the daemon. */
  async start(): Promise<void> {
    if (this.#running) {
      return;
    }
    this.#running = true;
    await this.refresh();
    this.#statusTimer = setInterval(() => void this.refresh(), STATUS_INTERVAL);
    void this.#watch();
  }

  /** stop releases the subscriptions of the shell. */
  stop(): void {
    this.#running = false;
    if (this.#statusTimer) {
      clearInterval(this.#statusTimer);
      this.#statusTimer = null;
    }
    this.#stream?.abort();
    this.#stream = null;
  }

  /**
   * onEvent subscribes to the daemon events. The returned function removes the
   * subscription.
   */
  onEvent(handler: (event: DaemonEvent) => void): () => void {
    this.#events.add(handler);
    return () => {
      this.#events.delete(handler);
    };
  }

  /** refresh reloads the status of the daemon. */
  async refresh(): Promise<void> {
    try {
      this.status = await client.getStatus({});
      this.error = null;
      this.startup = { state: "running", reason: null };
    } catch (error) {
      this.status = null;
      this.error = messageOf(error);
      await this.#loadStart();
    }
    this.#syncTray();
  }

  /** setPause applies a pause choice and reloads the status. */
  async setPause(choice: PauseChoice): Promise<void> {
    try {
      switch (choice) {
        case "five_minutes":
          await client.setPause({ mode: PauseMode.UNTIL, untilUnixMs: until(FIVE_MINUTES) });
          break;
        case "one_hour":
          await client.setPause({ mode: PauseMode.UNTIL, untilUnixMs: until(ONE_HOUR) });
          break;
        case "indefinite":
          await client.setPause({ mode: PauseMode.INDEFINITE });
          break;
        case "off":
          await client.setPause({ mode: PauseMode.OFF });
          break;
      }
    } catch (error) {
      this.error = messageOf(error);
    }
    await this.refresh();
  }

  /** #loadStart reads why the app could not start the daemon. */
  async #loadStart(): Promise<void> {
    try {
      this.startup = await daemonStatus();
    } catch (error) {
      // The state of an earlier status stays, because the command is the only
      // source of the reason and it is missing only outside the app window.
      console.warn("cannot read the state of the daemon", error);
    }
  }

  #syncTray(): void {
    const state = {
      connected: this.connected,
      onlinePeers: this.onlinePeers,
      paused: this.paused,
      devices: this.devices
        .filter((device) => device.online && !device.revoked)
        .map((device) => ({ id: device.deviceId, name: device.name || shortDeviceID(device.deviceId) })),
    };
    void updateTray(state).catch((error: unknown) => {
      console.warn("cannot update the tray", error);
    });
  }

  async #watch(): Promise<void> {
    while (this.#running) {
      const controller = new AbortController();
      this.#stream = controller;
      try {
        for await (const response of client.watchEvents({}, { signal: controller.signal })) {
          const event = response.event;
          if (!event) {
            continue;
          }
          this.#record(event);
          if (event.kind === EventKind.PEER_ONLINE || event.kind === EventKind.PEER_OFFLINE) {
            void this.refresh();
          }
        }
      } catch (error) {
        if (!this.#running) {
          return;
        }
        this.error = messageOf(error);
      } finally {
        if (this.#stream === controller) {
          this.#stream = null;
        }
      }
      if (!this.#running) {
        return;
      }
      await sleep(RETRY_DELAY);
    }
  }

  #record(event: DaemonEvent): void {
    this.activity = [event, ...this.activity].slice(0, ACTIVITY_LIMIT);
    for (const handler of this.#events) {
      handler(event);
    }
  }
}

function until(offsetMs: number): bigint {
  return BigInt(Date.now() + offsetMs);
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function messageOf(error: unknown): string {
  if (error instanceof ConnectError) {
    return error.rawMessage;
  }
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

/** daemon is the state of the shell. */
export const daemon = new DaemonStore();
