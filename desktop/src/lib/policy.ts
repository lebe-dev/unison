import type {
  PolicyCell as ProtoCell,
  PolicyRow as ProtoRow,
  Settings,
} from "./api/gen/unison/local/v1/local_pb";
import { PolicyAction } from "./api/gen/unison/local/v1/local_pb";
import type { MessageKey } from "./i18n";

/** One cell of the policy table. */
export type PolicyCell = {
  action: PolicyAction;
  maxSizeBytes: bigint;
};

/** One row of the policy table. */
export type PolicyRow = {
  text: PolicyCell;
  image: PolicyCell;
  files: PolicyCell;
  relay: boolean;
};

/** The policy table of the daemon. */
export type PolicyTable = {
  unmetered: PolicyRow;
  metered: PolicyRow;
  roaming: PolicyRow;
  dataSaver: PolicyRow;
  relayMaxSizeBytes: bigint;
};

/** Network condition of one row. */
export type PolicyCondition = "unmetered" | "metered" | "roaming" | "dataSaver";

/** Content kind of one cell. */
export type PolicyKind = "text" | "image" | "files";

/** Rows of the table in the order of the screen. */
export const conditions: { key: PolicyCondition; label: MessageKey; hint: MessageKey }[] = [
  { key: "unmetered", label: "policy.condition.unmetered", hint: "policy.hint.unmetered" },
  { key: "metered", label: "policy.condition.metered", hint: "policy.hint.metered" },
  { key: "roaming", label: "policy.condition.roaming", hint: "policy.hint.roaming" },
  { key: "dataSaver", label: "policy.condition.dataSaver", hint: "policy.hint.dataSaver" },
];

/** Cells of a row in the order of the screen. */
export const kinds: { key: PolicyKind; label: MessageKey }[] = [
  { key: "text", label: "policy.kind.text" },
  { key: "image", label: "policy.kind.image" },
  { key: "files", label: "policy.kind.files" },
];

/** Outcomes of a cell. */
export const actions: { value: PolicyAction; label: MessageKey }[] = [
  { value: PolicyAction.REJECT, label: "policy.action.reject" },
  { value: PolicyAction.METADATA, label: "policy.action.metadata" },
  { value: PolicyAction.CONFIRM, label: "policy.action.confirm" },
  { value: PolicyAction.AUTO, label: "policy.action.auto" },
];

/** Bytes in one megabyte, the unit of the size fields. */
export const megabyte = 1n << 20n;

/** Sizes of the table in whole megabytes, at least one. */
export function megabytesOf(bytes: bigint): number {
  const value = Math.round(Number(bytes) / Number(megabyte));
  return Number.isFinite(value) && value > 0 ? value : 1;
}

/** Bytes of whole megabytes, at least one. */
export function bytesOf(megabytes: number): bigint {
  const whole = Math.round(megabytes);
  return BigInt(Number.isFinite(whole) && whole > 0 ? whole : 1) * megabyte;
}

/** Table of the settings, null when the daemon does not report one. */
export function policyFromSettings(settings: Settings | undefined): PolicyTable | null {
  if (!settings) {
    return null;
  }
  const unmetered = rowFromProto(settings.unmetered);
  const metered = rowFromProto(settings.metered);
  const roaming = rowFromProto(settings.roaming);
  const dataSaver = rowFromProto(settings.dataSaver);
  if (!unmetered || !metered || !roaming || !dataSaver) {
    return null;
  }
  return {
    unmetered,
    metered,
    roaming,
    dataSaver,
    relayMaxSizeBytes: settings.relayMaxSizeBytes ?? 0n,
  };
}

/** Request fields of the whole table. */
export function policyRequest(table: PolicyTable): PolicyRequest {
  return {
    unmetered: rowToRequest(table.unmetered),
    metered: rowToRequest(table.metered),
    roaming: rowToRequest(table.roaming),
    dataSaver: rowToRequest(table.dataSaver),
    relayMaxSizeBytes: table.relayMaxSizeBytes,
  };
}

/** Copy of the table with another cell. */
export function withCell(
  table: PolicyTable,
  condition: PolicyCondition,
  kind: PolicyKind,
  cell: PolicyCell,
): PolicyTable {
  return { ...table, [condition]: { ...table[condition], [kind]: cell } };
}

/** Copy of the table with another relay permission. */
export function withRelay(
  table: PolicyTable,
  condition: PolicyCondition,
  relay: boolean,
): PolicyTable {
  return { ...table, [condition]: { ...table[condition], relay } };
}

/** One cell of a request. */
type PolicyCellRequest = {
  action: PolicyAction;
  maxSizeBytes: bigint;
};

/** One row of a request. */
type PolicyRowRequest = {
  text: PolicyCellRequest;
  image: PolicyCellRequest;
  files: PolicyCellRequest;
  relay: boolean;
};

/** Request fields of the policy table. */
export type PolicyRequest = {
  unmetered: PolicyRowRequest;
  metered: PolicyRowRequest;
  roaming: PolicyRowRequest;
  dataSaver: PolicyRowRequest;
  relayMaxSizeBytes: bigint;
};

/** One row of a request. */
function rowToRequest(row: PolicyRow): PolicyRowRequest {
  const cell = (value: PolicyCell): PolicyCellRequest => ({
    action: value.action,
    maxSizeBytes: value.maxSizeBytes,
  });
  return {
    text: cell(row.text),
    image: cell(row.image),
    files: cell(row.files),
    relay: row.relay,
  };
}

/** Row of the settings, null when it is missing. */
function rowFromProto(row: ProtoRow | undefined): PolicyRow | null {
  if (!row) {
    return null;
  }
  const text = cellFromProto(row.text);
  const image = cellFromProto(row.image);
  const files = cellFromProto(row.files);
  if (!text || !image || !files || row.relay === undefined) {
    return null;
  }
  return { text, image, files, relay: row.relay };
}

/** Cell of the settings, null when it is missing. */
function cellFromProto(cell: ProtoCell | undefined): PolicyCell | null {
  if (!cell?.action || cell.maxSizeBytes === undefined) {
    return null;
  }
  return { action: cell.action, maxSizeBytes: cell.maxSizeBytes };
}
