import { FailureReason, type Device, type Event } from "./api/gen/unison/local/v1/local_pb";
import { cleanError } from "./errors";
import { formatDateTime } from "./format";
import { translate } from "./i18n";

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
      message: translate("network.unreachable"),
      hint: translate("network.unreachableHint"),
      details: event.error,
    };
  }
  return {
    paired: false,
    message: cleanError(event.error) || translate("network.pairingFailed"),
  };
}

/** unreachableDeviceIssue describes a paired device that does not answer. */
export function unreachableDeviceIssue(device: Device): NetworkIssueText | null {
  if (device.online || !device.unreachable) {
    return null;
  }
  const name = device.name || translate("common.theDevice");
  const lines = [];
  if (device.unreachableSinceUnixMs) {
    lines.push(
      translate("network.notAnsweringSince", {
        time: formatDateTime(device.unreachableSinceUnixMs),
      }),
    );
  }
  if (device.unreachableDetail) {
    lines.push(device.unreachableDetail);
  }
  return {
    summary: translate("network.deviceDoesNotAnswer", { name }),
    hint: translate("network.deviceOffHint"),
    details: lines.join("\n") || undefined,
  };
}

/**
 * quietWindowIssue warns about an open pairing window that has not seen any
 * device after quietWindowDelay.
 */
export function quietWindowIssue(openFor: number, addr: string): NetworkIssueText | null {
  if (openFor < quietWindowDelay) {
    return null;
  }
  const seconds = Math.floor(openFor / 1000);
  const lines = [translate("network.noAnnouncement", { count: seconds })];
  if (addr) {
    lines.push(translate("network.windowListensAt", { addr }));
  }
  return {
    summary: translate("network.noDevicesFound"),
    hint: translate("network.noDevicesFoundHint"),
    details: lines.join("\n"),
  };
}
