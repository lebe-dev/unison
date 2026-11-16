import { EventKind } from "./api/gen/unison/local/v1/local_pb";

const LABELS: Record<number, string> = {
  [EventKind.PEER_ONLINE]: "Device online",
  [EventKind.PEER_OFFLINE]: "Device offline",
  [EventKind.CLIPBOARD]: "Clipboard updated",
  [EventKind.PEER_INCOMPATIBLE]: "Device needs an update",
  [EventKind.PAIRING_CANDIDATE]: "Pairing candidate found",
  [EventKind.PAIRING_REQUEST]: "Pairing request",
  [EventKind.PAIRING_PAIRED]: "Device paired",
  [EventKind.PAIRING_FAILED]: "Pairing failed",
  [EventKind.PAIRING_CLOSED]: "Pairing window closed",
  [EventKind.CLIP_DOWNLOAD]: "Item waiting for download",
  [EventKind.CLIP_APPLY]: "Item waiting for confirmation",
  [EventKind.TRANSFER]: "Transfer updated",
};

/** eventKindLabel returns a short English label of a daemon event. */
export function eventKindLabel(kind: EventKind): string {
  return LABELS[kind] ?? "Event";
}
