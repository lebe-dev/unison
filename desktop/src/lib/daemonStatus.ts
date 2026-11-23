import { invoke } from "@tauri-apps/api/core";

/** State of the daemon as the app knows it. */
export type DaemonState = "running" | "offline" | "failed";

/**
 * Installation the app can offer: the daemon of Windows is missing, or the app
 * has no copy of it to install.
 */
export type DaemonInstall = "off" | "available" | "missing";

/** Status of the daemon that the app keeps. */
export interface DaemonStatus {
  state: DaemonState;
  /** Reason of the last failed start, absent when it is not known. */
  reason: string | null;
  /** Whether the app can install the daemon here. */
  install: DaemonInstall;
}

/**
 * daemonStatus returns the state of the daemon, the reason of the last failed
 * start and the installation the app can offer.
 */
export function daemonStatus(): Promise<DaemonStatus> {
  return invoke<DaemonStatus>("daemon_status");
}

/**
 * installDaemon puts the daemon in place and starts it. The app asks for the
 * elevation once; nothing happens when the user refuses it.
 */
export function installDaemon(): Promise<void> {
  return invoke<void>("install_daemon");
}
