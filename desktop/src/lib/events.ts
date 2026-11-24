import { EventKind } from "./api/gen/unison/local/v1/local_pb";
import { translate, type MessageKey } from "./i18n";

const LABELS: Record<number, MessageKey> = {
  [EventKind.PEER_ONLINE]: "events.peerOnline",
  [EventKind.PEER_OFFLINE]: "events.peerOffline",
  [EventKind.CLIPBOARD]: "events.clipboard",
  [EventKind.PEER_INCOMPATIBLE]: "events.peerIncompatible",
  [EventKind.PAIRING_CANDIDATE]: "events.pairingCandidate",
  [EventKind.PAIRING_REQUEST]: "events.pairingRequest",
  [EventKind.PAIRING_PAIRED]: "events.pairingPaired",
  [EventKind.PAIRING_FAILED]: "events.pairingFailed",
  [EventKind.PAIRING_CLOSED]: "events.pairingClosed",
  [EventKind.CLIP_DOWNLOAD]: "events.clipDownload",
  [EventKind.CLIP_APPLY]: "events.clipApply",
  [EventKind.TRANSFER]: "events.transfer",
  [EventKind.DEVICE_UPDATED]: "events.deviceUpdated",
  [EventKind.PEER_UNREACHABLE]: "events.peerUnreachable",
  [EventKind.MEDIATED_PAIR_OFFER]: "events.mediatedPairOffer",
  [EventKind.MEDIATED_PAIR_UPDATED]: "events.mediatedPairUpdated",
  [EventKind.MEDIATED_PAIR_COMPLETED]: "events.mediatedPairCompleted",
  [EventKind.MEDIATED_PAIR_CANCELED]: "events.mediatedPairCanceled",
};

/** eventKindLabel returns a short label of a daemon event. */
export function eventKindLabel(kind: EventKind): string {
  return translate(LABELS[kind] ?? "events.event");
}
