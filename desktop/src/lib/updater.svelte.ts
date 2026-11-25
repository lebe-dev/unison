import type { UnlistenFn } from "@tauri-apps/api/event";
import {
  cancelUpdate,
  checkNow,
  dismissUpdate,
  getUpdaterState,
  installUpdate,
  onUpdaterState,
  setConfig,
  type ConfigChanges,
  type ReleaseSummary,
  type UpdateStatus,
  type UpdaterConfig,
  type UpdaterSnapshot,
} from "./updater";

/** Settings the updater uses before the shell answers. */
export const DEFAULT_UPDATER_CONFIG: UpdaterConfig = {
  enabled: false,
  betaChannel: false,
  interval: "daily",
  lastCheckedUnixMs: null,
  dismissedVersion: null,
  lastEtag: null,
};

/** Kind of a status that keeps the updater busy. */
const BUSY_KINDS = new Set(["checking", "downloading", "verifying", "installing", "restarting"]);

/** The dialog stays open through these phases, because the app may restart. */
const LOCKED_KINDS = new Set(["installing", "restarting"]);

/**
 * UpdaterStore keeps the state of the updater and the dialog. The shell checks
 * in the background, so the window only reads the snapshot and calls the
 * commands.
 */
class UpdaterStore {
  /** Last state the shell reported. */
  snapshot = $state<UpdaterSnapshot | null>(null);
  /** Whether the update dialog is open. */
  dialogOpen = $state(false);
  /** Reason of the last action the shell refused. */
  failure = $state<string | null>(null);

  #started = false;
  #unlisten: UnlistenFn | null = null;

  /** config returns the settings of the updater. */
  get config(): UpdaterConfig {
    return this.snapshot?.config ?? DEFAULT_UPDATER_CONFIG;
  }

  /** status returns the state of the last check or installation. */
  get status(): UpdateStatus {
    return this.snapshot?.status ?? { kind: "idle" };
  }

  /** available returns the release the shell offers. */
  get available(): ReleaseSummary | null {
    return this.snapshot?.available ?? null;
  }

  /** currentVersion returns the version of this build. */
  get currentVersion(): string {
    return this.snapshot?.currentVersion ?? "";
  }

  /** supported reports whether this build can install an update itself. */
  get supported(): boolean {
    return this.snapshot?.supported ?? false;
  }

  /** unsupportedReason explains why this build cannot install an update. */
  get unsupportedReason(): string | null {
    return this.snapshot?.unsupportedReason ?? null;
  }

  /** hasUpdate reports whether a newer release is offered. */
  get hasUpdate(): boolean {
    return this.available !== null;
  }

  /** dismissed reports whether the user put the offered release aside. */
  get dismissed(): boolean {
    return this.snapshot?.dismissed ?? false;
  }

  /** showBanner reports whether the dashboard shows the banner. */
  get showBanner(): boolean {
    return this.hasUpdate && !this.dismissed;
  }

  /** busy reports whether a check, a download or an installation runs. */
  get busy(): boolean {
    return BUSY_KINDS.has(this.status.kind);
  }

  /** error returns the error of the last check or installation. */
  get error(): { code: string; message: string } | null {
    return this.status.kind === "error"
      ? { code: this.status.code, message: this.status.message }
      : null;
  }

  /** start reads the state and follows the shell. */
  start(): void {
    if (this.#started) {
      return;
    }
    this.#started = true;
    void onUpdaterState((snapshot) => {
      this.snapshot = snapshot;
    })
      .then((unlisten) => {
        if (this.#started) {
          this.#unlisten = unlisten;
          return;
        }
        // A stop before the subscription returned releases it at once.
        void unlisten();
      })
      .catch((error: unknown) => {
        this.failure = String(error);
      });
    void this.refresh();
  }

  /** stop releases the subscription. */
  stop(): void {
    this.#started = false;
    void this.#unlisten?.();
    this.#unlisten = null;
  }

  /** refresh reads the state of the updater. */
  async refresh(): Promise<void> {
    try {
      this.snapshot = await getUpdaterState();
      this.failure = null;
    } catch (error) {
      this.failure = String(error);
    }
  }

  /** setEnabled turns the background checks on or off. */
  setEnabled(enabled: boolean): Promise<void> {
    return this.apply({ enabled });
  }

  /** setBetaChannel includes or excludes the GitHub pre-releases. */
  setBetaChannel(betaChannel: boolean): Promise<void> {
    return this.apply({ betaChannel });
  }

  /** setInterval changes how often the background checks run. */
  setInterval(interval: UpdaterConfig["interval"]): Promise<void> {
    return this.apply({ interval });
  }

  /** setConfig changes several settings at once. */
  setConfig(changes: ConfigChanges): Promise<void> {
    return this.apply(changes);
  }

  /** checkNow asks the shell for a check, even when background checks are off. */
  async checkNow(): Promise<void> {
    this.failure = null;
    try {
      this.snapshot = await checkNow();
    } catch (error) {
      this.failure = String(error);
    }
  }

  /** dismiss puts the offered release aside. */
  async dismiss(): Promise<void> {
    const version = this.available?.version;
    if (!version) {
      return;
    }
    try {
      this.snapshot = await dismissUpdate(version);
    } catch (error) {
      this.failure = String(error);
    }
  }

  /** install starts the download and the installation of the update. */
  async install(): Promise<void> {
    this.failure = null;
    try {
      await installUpdate();
    } catch (error) {
      this.failure = String(error);
    }
  }

  /** cancel stops a running download. */
  async cancel(): Promise<void> {
    try {
      await cancelUpdate();
    } catch (error) {
      this.failure = String(error);
    }
  }

  /** openDialog shows the update dialog. */
  openDialog(): void {
    if (this.hasUpdate) {
      this.dialogOpen = true;
    }
  }

  /** closeDialog hides the dialog unless an installation is running. */
  closeDialog(): void {
    if (!LOCKED_KINDS.has(this.status.kind)) {
      this.dialogOpen = false;
    }
  }

  /** apply sends changed settings and stores the answer. */
  async apply(changes: ConfigChanges): Promise<void> {
    this.failure = null;
    try {
      this.snapshot = await setConfig(changes);
    } catch (error) {
      this.failure = String(error);
    }
  }
}

/** updater is the updater state of the app. */
export const updater = new UpdaterStore();
