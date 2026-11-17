import {
  PolicyAction,
  RelayTransport,
  type NetworkState,
  type Relay,
  type Settings,
} from "./api/gen/unison/local/v1/local_pb";

/** Levels of a journal record, in the order of the format. */
export const journalLevels = ["DEBUG", "INFO", "WARN", "ERROR"] as const;

/** Level of a journal record. */
export type JournalLevel = (typeof journalLevels)[number];

/** Level the window shows: INFO hides the debug records. */
export type DisplayLevel = "info" | "debug";

/** Number of records the window renders, newest first. */
export const MAX_VISIBLE_RECORDS = 500;

/** One record of the application log. */
export interface JournalRecord {
  /** UTC timestamp with milliseconds. */
  at: string;
  level: JournalLevel;
  message: string;
  /** The record as it is written in the file. */
  text: string;
}

/** What the header of the export describes. */
export interface HeaderInput {
  generated: Date;
  appVersion: string;
  daemonVersion: string | null;
  os: string;
  deviceName: string;
  deviceId: string;
  network: NetworkState | undefined;
  addresses: string[];
  settings: Settings;
  relay: Relay;
  /** The marker exists, so the journal records even without the daemon. */
  journalMarker: boolean;
}

/** RECORD matches a line of the application log. */
const RECORD = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z) (DEBUG|INFO|WARN|ERROR) (.*)$/;

/** Actions of a policy cell by their proto value. */
const actionNames: Record<number, string> = {
  [PolicyAction.REJECT]: "reject",
  [PolicyAction.METADATA]: "metadata",
  [PolicyAction.CONFIRM]: "confirm",
  [PolicyAction.AUTO]: "auto",
};

/** Conditions of the policy table with the names of the export. */
const conditions = [
  { key: "unmetered", name: "unmetered" },
  { key: "metered", name: "metered" },
  { key: "roaming", name: "roaming" },
  { key: "dataSaver", name: "data_saver" },
] as const;

/** Kinds of a policy row with the names of the export. */
const kinds = ["text", "image", "files"] as const;

/** parseRecord returns the record of a line, null when the line is not one. */
export function parseRecord(line: string): JournalRecord | null {
  const match = RECORD.exec(line);
  if (!match) {
    return null;
  }
  return { at: match[1], level: match[2] as JournalLevel, message: match[3], text: line };
}

/** recordsFrom returns the records of the lines, oldest first. */
export function recordsFrom(lines: string[]): JournalRecord[] {
  return lines
    .map(parseRecord)
    .filter((record): record is JournalRecord => record !== null)
    .sort((left, right) => (left.at < right.at ? -1 : left.at > right.at ? 1 : 0));
}

/** visibleRecords returns the newest records of a display level, newest first. */
export function visibleRecords(records: JournalRecord[], level: DisplayLevel): JournalRecord[] {
  const shown = level === "debug" ? records : records.filter((record) => record.level !== "DEBUG");
  return shown.slice(-MAX_VISIBLE_RECORDS).reverse();
}

/** relayUrl returns the address of the relay without credentials or a query. */
export function relayUrl(url: string): string {
  const withoutQuery = url.trim().split("?")[0] ?? "";
  const schemeEnd = withoutQuery.indexOf("://");
  const authorityStart = schemeEnd >= 0 ? schemeEnd + 3 : 0;
  const pathStart = withoutQuery.indexOf("/", authorityStart);
  const authorityEnd = pathStart >= 0 ? pathStart : withoutQuery.length;
  const authority = withoutQuery.slice(authorityStart, authorityEnd);
  const at = authority.lastIndexOf("@");
  if (at < 0) {
    return withoutQuery;
  }
  return (
    withoutQuery.slice(0, authorityStart) +
    authority.slice(at + 1) +
    withoutQuery.slice(authorityEnd)
  );
}

/** relayTransportName returns the name of the transport of the relay. */
export function relayTransportName(transport: RelayTransport): string {
  switch (transport) {
    case RelayTransport.QUIC:
      return "quic";
    case RelayTransport.WSS:
      return "wss";
    default:
      return "none";
  }
}

/** networkCondition returns the name of the network condition of the daemon. */
export function networkCondition(network: NetworkState | undefined): string {
  if (!network?.online) {
    return "offline";
  }
  if (network.dataSaver) {
    return "data_saver";
  }
  if (network.roaming) {
    return "roaming";
  }
  if (network.metered) {
    return "metered";
  }
  return "unmetered";
}

/** actionName returns the settings name of a policy action. */
export function actionName(action: PolicyAction | undefined): string {
  return actionNames[action ?? PolicyAction.REJECT] ?? "reject";
}

/** composeHeader returns the header that describes the export. */
export function composeHeader(input: HeaderInput): string {
  const lines = [
    "unison application log",
    `generated: ${input.generated.toISOString()}`,
    `app: ${input.appVersion}`,
    `daemon: ${input.daemonVersion ?? "offline"}`,
    `os: ${input.os}`,
    `device: ${input.deviceName} (${input.deviceId})`,
    input.network?.online
      ? `network: online (${networkCondition(input.network)})`
      : "network: offline",
  ];
  if (input.addresses.length > 0) {
    lines.push(`addresses: ${input.addresses.join(", ")}`);
  }
  lines.push("settings:");
  lines.push(...settingsLines(input.settings, input.relay, input.journalMarker));
  return lines.join("\n");
}

/** settingsLines returns the settings of the header without secrets. */
export function settingsLines(settings: Settings, relay: Relay, journalMarker = false): string[] {
  const lines = [
    `  device_name: ${settings.deviceName}`,
    `  require_confirmation: ${settings.requireConfirmation}`,
    `  require_file_confirmation: ${settings.requireFileConfirmation}`,
    `  blob_max_size_bytes: ${settings.blobMaxSizeBytes}`,
    `  downloads_dir: [path]`,
    `  relay_max_size_bytes: ${settings.relayMaxSizeBytes}`,
    `  history_ttl_text_seconds: ${settings.historyTtlTextSeconds}`,
    `  history_ttl_image_seconds: ${settings.historyTtlImageSeconds}`,
    `  history_ttl_files_seconds: ${settings.historyTtlFilesSeconds}`,
    `  sentry_enabled: ${settings.sentryEnabled}`,
    `  strip_tracking: ${settings.stripTracking}`,
    `  journal_enabled: ${settings.journalEnabled || journalMarker}`,
    `  relay.url: ${relayUrl(relay.url)}`,
    `  relay.token_set: ${relay.tokenSet}`,
    `  relay.connected: ${relay.connected}`,
    `  relay.transport: ${relayTransportName(relay.transport)}`,
    `  relay.files_on_request: ${relay.filesOnRequest}`,
  ];
  for (const condition of conditions) {
    const row = settings[condition.key];
    for (const kind of kinds) {
      const cell = row?.[kind];
      lines.push(
        `  policy.${condition.name}.${kind}.action: ${actionName(cell?.action)}`,
        `  policy.${condition.name}.${kind}.max_size_bytes: ${cell?.maxSizeBytes ?? 0n}`,
      );
    }
    lines.push(`  policy.${condition.name}.relay: ${row?.relay ?? false}`);
  }
  return lines;
}

/** composeExport returns the shared file: the header and every record. */
export function composeExport(input: HeaderInput, lines: string[]): string {
  const records = recordsFrom(lines);
  const body = records.map((record) => record.text).join("\n");
  const header = composeHeader(input);
  return body ? `${header}\n---\n${body}\n` : `${header}\n---\n`;
}
