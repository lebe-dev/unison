import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";

/** Interval of the background checks. */
export type CheckInterval = "six_hours" | "daily" | "weekly";

/** Settings of the updater. */
export interface UpdaterConfig {
  enabled: boolean;
  betaChannel: boolean;
  interval: CheckInterval;
  lastCheckedUnixMs: number | null;
  dismissedVersion: string | null;
  lastEtag: string | null;
}

/** Step of an installation. */
export type InstallPhase =
  "preparing" | "stopping_daemon" | "applying_update" | "restarting_services" | "relaunching";

/** State of the updater. */
export type UpdateStatus =
  | { kind: "idle" }
  | { kind: "checking" }
  | { kind: "up_to_date" }
  | { kind: "available" }
  | { kind: "downloading"; downloaded: number; total: number }
  | { kind: "verifying" }
  | { kind: "installing"; phase: InstallPhase }
  | { kind: "restarting" }
  | { kind: "error"; code: string; message: string };

/** What the window shows about a release. */
export interface ReleaseSummary {
  version: string;
  name: string;
  body: string;
  prerelease: boolean;
  publishedAt: string;
  htmlUrl: string;
  assetSize: number;
}

/** State of the updater as the shell reports it. */
export interface UpdaterSnapshot {
  currentVersion: string;
  supported: boolean;
  unsupportedReason: string | null;
  config: UpdaterConfig;
  status: UpdateStatus;
  available: ReleaseSummary | null;
  dismissed: boolean;
}

/** The settings a caller may change. */
export interface ConfigChanges {
  enabled?: boolean;
  betaChannel?: boolean;
  interval?: CheckInterval;
}

/** getUpdaterState returns the state of the updater. */
export function getUpdaterState(): Promise<UpdaterSnapshot> {
  return invoke<UpdaterSnapshot>("updater_get_state");
}

/** setConfig changes the settings of the updater. */
export function setConfig(changes: ConfigChanges): Promise<UpdaterSnapshot> {
  return invoke<UpdaterSnapshot>("updater_set_config", {
    enabled: changes.enabled,
    betaChannel: changes.betaChannel,
    interval: changes.interval,
  });
}

/** checkNow checks for a release, even when background checks are off. */
export function checkNow(): Promise<UpdaterSnapshot> {
  return invoke<UpdaterSnapshot>("updater_check_now");
}

/** dismissUpdate hides the banner of a version. */
export function dismissUpdate(version: string): Promise<UpdaterSnapshot> {
  return invoke<UpdaterSnapshot>("updater_dismiss_update", { version });
}

/** installUpdate downloads, verifies and installs the offered release. */
export function installUpdate(): Promise<void> {
  return invoke<void>("updater_install");
}

/** cancelUpdate stops a running download. */
export function cancelUpdate(): Promise<void> {
  return invoke<void>("updater_cancel");
}

/** onUpdaterState follows the state of the updater. */
export function onUpdaterState(callback: (snapshot: UpdaterSnapshot) => void): Promise<UnlistenFn> {
  return listen<UpdaterSnapshot>("updater-state", (event) => callback(event.payload));
}
