import type { Settings } from "./api/gen/unison/local/v1/local_pb";

/** Kind of the history items a lifetime belongs to. */
export type TTLKind = "text" | "image" | "files";

/** Lifetimes of the history, in seconds. */
export type HistoryTTL = Record<TTLKind, bigint>;

/** Kinds in the order of the screen. */
export const ttlKinds: { key: TTLKind; label: string }[] = [
  { key: "text", label: "Text" },
  { key: "image", label: "Images" },
  { key: "files", label: "Files" },
];

/** Longest lifetime the daemon accepts. */
export const maxLifetime = 365n * 86400n;

/** Lifetimes of the settings, null when the daemon reports none. */
export function ttlFromSettings(settings: Settings | undefined): HistoryTTL | null {
  if (!settings) {
    return null;
  }
  return {
    text: settings.historyTtlTextSeconds,
    image: settings.historyTtlImageSeconds,
    files: settings.historyTtlFilesSeconds,
  };
}

/** Text form of a lifetime: "7d", "24h", "90m". */
export function lifetimeText(seconds: bigint): string {
  if (seconds <= 0n) {
    return "";
  }
  for (const [suffix, unit] of units) {
    if (seconds % unit === 0n) {
      return `${seconds / unit}${suffix}`;
    }
  }
  return `${seconds}s`;
}

/** Seconds of a lifetime like "7d", "24h" or "90m", null when it is not usable. */
export function parseLifetime(value: string): bigint | null {
  const text = value.trim().toLowerCase();
  for (const [suffix, unit] of units) {
    if (!text.endsWith(suffix)) {
      continue;
    }
    const count = text.slice(0, -suffix.length);
    if (!/^\d+$/.test(count)) {
      return null;
    }
    const seconds = BigInt(count) * unit;
    if (seconds <= 0n || seconds > maxLifetime) {
      return null;
    }
    return seconds;
  }
  return null;
}

/** Request field that changes the lifetime of one kind. */
export function ttlRequest(
  kind: TTLKind,
  seconds: bigint,
): {
  historyTtlTextSeconds?: bigint;
  historyTtlImageSeconds?: bigint;
  historyTtlFilesSeconds?: bigint;
} {
  switch (kind) {
    case "image":
      return { historyTtlImageSeconds: seconds };
    case "files":
      return { historyTtlFilesSeconds: seconds };
    default:
      return { historyTtlTextSeconds: seconds };
  }
}

/** Units of a lifetime, longest first. */
const units: [string, bigint][] = [
  ["d", 86400n],
  ["h", 3600n],
  ["m", 60n],
  ["s", 1n],
];
