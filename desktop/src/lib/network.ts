import {
  FailureReason,
  type Device,
  type Event,
} from "./api/gen/unison/local/v1/local_pb";
import { cleanError } from "./errors";
import { formatDateTime } from "./format";

/**
 * NetworkIssueText is a connectivity problem in two levels: a short message in
 * plain words and the technical details behind a toggle.
 */
export interface NetworkIssueText {
  summary: string;
  hint?: string;
  details?: string;
}

/** Outcome of a pairing exchange that ended without a new device. */
export interface PairingFailure {
  paired: false;
  message: string;
  hint?: string;
  details?: string;
}

/** quietWindowDelay is how long an open window waits before it warns. */
export const quietWindowDelay = 30_000;

/** pairingFailure describes the failure event of a pairing exchange. */
export function pairingFailure(event: Event): PairingFailure {
  if (event.failureReason === FailureReason.UNREACHABLE) {
    return {
      paired: false,
      message: "Could not reach the other device.",
      hint:
        "Keep both devices on the same network. Guest Wi-Fi and some routers keep their " +
        "devices apart, and a VPN can do the same.",
      details: event.error,
    };
  }
  return {
    paired: false,
    message: cleanError(event.error) || "The pairing exchange failed.",
  };
}

/** unreachableDeviceIssue describes a paired device that does not answer. */
export function unreachableDeviceIssue(
  device: Device,
): NetworkIssueText | null {
  if (device.online || !device.unreachable) {
    return null;
  }
  const name = device.name || "The device";
  const lines = [];
  if (device.unreachableSinceUnixMs) {
    lines.push(
      `Not answering since ${formatDateTime(device.unreachableSinceUnixMs)}.`,
    );
  }
  if (device.unreachableDetail) {
    lines.push(device.unreachableDetail);
  }
  return {
    summary: `${name} does not answer.`,
    hint:
      "It may be turned off. If it is on, check that both devices are on the same network " +
      "and that the network does not keep its devices apart.",
    details: lines.join("\n") || undefined,
  };
}

/**
 * quietWindowIssue warns about an open pairing window that has not seen any
 * device after quietWindowDelay.
 */
export function quietWindowIssue(
  openFor: number,
  addr: string,
): NetworkIssueText | null {
  if (openFor < quietWindowDelay) {
    return null;
  }
  const seconds = Math.floor(openFor / 1000);
  const lines = [`No pairing announcement arrived in ${seconds} seconds.`];
  if (addr) {
    lines.push(`This window listens at ${addr}.`);
  }
  return {
    summary: "No devices found on this network yet.",
    hint:
      "Open the pairing window on the other device. If it is open, this network may keep the " +
      "devices apart: connect both to another network, or to the same VPN.",
    details: lines.join("\n"),
  };
}
