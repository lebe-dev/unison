import { invoke } from "@tauri-apps/api/core";

/** State of the daemon as the app knows it. */
export type DaemonState = "running" | "offline" | "failed";

/** Status of the daemon that the app keeps. */
export interface DaemonStatus {
  state: DaemonState;
  /** Reason of the last failed start, absent when it is not known. */
  reason: string | null;
}

/**
 * daemonStatus returns the state of the daemon and the reason of the last
 * failed start.
 */
export function daemonStatus(): Promise<DaemonStatus> {
  return invoke<DaemonStatus>("daemon_status");
}
