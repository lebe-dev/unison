import {
  Kind,
  Path,
  Platform,
  Role,
  TransferDirection,
  TransferState,
  type ClipItem,
  type Representation,
  type Transfer,
} from "./api/gen/unison/local/v1/local_pb";

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

const KIND_LABELS: Record<number, string> = {
  [Kind.TEXT]: "Text",
  [Kind.IMAGE]: "Image",
  [Kind.FILES]: "Files",
};

const TRANSFER_STATE_LABELS: Record<number, string> = {
  [TransferState.PENDING]: "Waiting for an answer",
  [TransferState.OFFERED]: "Offered",
  [TransferState.TRANSFERRING]: "Transferring",
  [TransferState.DONE]: "Done",
  [TransferState.DECLINED]: "Declined",
  [TransferState.CANCELED]: "Canceled",
  [TransferState.FAILED]: "Failed",
};

const TRANSFER_DIRECTION_LABELS: Record<number, string> = {
  [TransferDirection.OUTGOING]: "Sent",
  [TransferDirection.INCOMING]: "Received",
};

/** kindLabel returns the name of a clip kind. */
export function kindLabel(kind: Kind): string {
  return KIND_LABELS[kind] ?? "Item";
}

/** transferStateLabel returns the name of a transfer state. */
export function transferStateLabel(state: TransferState): string {
  return TRANSFER_STATE_LABELS[state] ?? "Unknown state";
}

/** transferDirectionLabel returns the direction of a transfer from the point
 * of view of this device. */
export function transferDirectionLabel(direction: TransferDirection): string {
  return TRANSFER_DIRECTION_LABELS[direction] ?? "";
}

const SIZE_UNITS = ["B", "KB", "MB", "GB", "TB"];

/** formatBytes renders a byte count with a unit. */
export function formatBytes(value: bigint | number): string {
  const bytes = typeof value === "bigint" ? Number(value) : value;
  if (!Number.isFinite(bytes) || bytes < 0) {
    return "";
  }
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  let size = bytes;
  let unit = 0;
  while (size >= 1024 && unit < SIZE_UNITS.length - 1) {
    size /= 1024;
    unit += 1;
  }
  const digits = size >= 100 ? 0 : 1;
  return `${size.toFixed(digits)} ${SIZE_UNITS[unit]}`;
}

/** itemSize returns the total size of the payloads of a clip item. */
export function itemSize(item: ClipItem | undefined): number {
  if (!item) {
    return 0;
  }
  return item.representations.reduce((total, rep) => total + Number(rep.size), 0);
}

/** representation returns the first payload of the item with the given MIME
 * prefix. */
function representation(item: ClipItem, prefix: string): Representation | undefined {
  return item.representations.find((rep) => rep.mimeType.startsWith(prefix));
}

/** decodeText decodes the payload of a text representation. */
function decodeText(rep: Representation | undefined): string {
  if (!rep || rep.inlineBytes.length === 0) {
    return "";
  }
  return new TextDecoder().decode(rep.inlineBytes);
}

/** textSnippet returns the inline text of an item on a single line. */
export function textSnippet(item: ClipItem | undefined, limit = 80): string {
  if (!item) {
    return "";
  }
  const text =
    decodeText(representation(item, "text/plain")) || decodeText(representation(item, "text/html"));
  const line = text.replace(/\s+/g, " ").trim();
  if (line.length <= limit) {
    return line;
  }
  return `${line.slice(0, limit - 1).trimEnd()}...`;
}

/** historyTitle returns a short title of a history entry. It never contains
 * more than a few words of a text item. */
export function historyTitle(item: ClipItem | undefined): string {
  if (!item) {
    return "";
  }
  switch (item.kind) {
    case Kind.TEXT: {
      const snippet = textSnippet(item, 60);
      return snippet || "Text item";
    }
    case Kind.IMAGE:
      return `Image - ${formatBytes(itemSize(item))}`;
    case Kind.FILES: {
      const names = item.representations.map((rep) => rep.fileName).filter((name) => name !== "");
      if (names.length === 0) {
        return "Files";
      }
      if (names.length === 1) {
        return names[0];
      }
      return `${names[0]} and ${names.length - 1} more`;
    }
    default:
      return "Item";
  }
}

/** fileNames returns the file names of a history entry. */
export function fileNames(item: ClipItem | undefined): string[] {
  if (!item) {
    return [];
  }
  return item.representations.map((rep) => rep.fileName).filter((name) => name !== "");
}

/** dataURL turns an inline payload into an URL the webview can show. */
export function dataURL(mime: string, bytes: Uint8Array): string {
  let binary = "";
  const step = 0x8000;
  for (let offset = 0; offset < bytes.length; offset += step) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + step));
  }
  return `data:${mime};base64,${btoa(binary)}`;
}

/** imagePreview returns the inline image of an item as an URL, or null when
 * the payload is not stored inline. */
export function imagePreview(item: ClipItem | undefined): string | null {
  if (!item || item.kind !== Kind.IMAGE) {
    return null;
  }
  const rep = representation(item, "image/");
  if (!rep || rep.inlineBytes.length === 0) {
    return null;
  }
  return dataURL(rep.mimeType, rep.inlineBytes);
}

/** progressPercent returns the share of the transferred bytes, 0 when the size
 * is unknown. */
export function progressPercent(transfer: Transfer): number {
  const total = Number(transfer.totalBytes);
  if (total <= 0) {
    return 0;
  }
  const done = Number(transfer.bytes);
  return Math.min(100, Math.max(0, Math.round((done / total) * 100)));
}

/** transferFilesText returns a short description of the files of a
 * transfer. */
export function transferFilesText(transfer: Transfer): string {
  if (transfer.files.length === 1) {
    return transfer.files[0].name;
  }
  return `${transfer.files.length} files`;
}
