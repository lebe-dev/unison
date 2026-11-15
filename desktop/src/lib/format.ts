import { Path, Platform, Role } from "./api/gen/unison/local/v1/local_pb";

const ROLE_LABELS: Record<number, string> = {
  [Role.PERSONAL]: "Personal",
  [Role.FRIENDS]: "Friends",
};

const PLATFORM_LABELS: Record<number, string> = {
  [Platform.MACOS]: "macOS",
  [Platform.WINDOWS]: "Windows",
  [Platform.ANDROID]: "Android",
};

const PATH_LABELS: Record<number, string> = {
  [Path.LAN]: "LAN",
  [Path.RELAY]: "Relay",
};

/** roleLabel returns the name of a device role. */
export function roleLabel(role: Role): string {
  return ROLE_LABELS[role] ?? "Unknown role";
}

/** platformLabel returns the name of the operating system of a device. */
export function platformLabel(platform: Platform): string {
  return PLATFORM_LABELS[platform] ?? "Unknown system";
}

/** pathLabel returns the connection path to a device, or an empty string when
 * the device is offline. */
export function pathLabel(path: Path): string {
  return PATH_LABELS[path] ?? "";
}

/** shortDeviceID groups the first characters of a device ID for reading. */
export function shortDeviceID(deviceID: string): string {
  const head = deviceID.slice(0, 12).toUpperCase();
  return head.replace(/(.{4})(?=.)/g, "$1 ");
}

/** formatTime renders a unix millisecond timestamp as the local time of day. */
export function formatTime(value: bigint | number): string {
  const ms = typeof value === "bigint" ? Number(value) : value;
  if (!ms) {
    return "";
  }
  return new Date(ms).toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** formatDateTime renders a unix millisecond timestamp as a local date and
 * time. */
export function formatDateTime(value: bigint | number): string {
  const ms = typeof value === "bigint" ? Number(value) : value;
  if (!ms) {
    return "";
  }
  return new Date(ms).toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });
}
