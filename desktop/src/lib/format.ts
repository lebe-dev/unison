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
import { getLocale, translate, type MessageKey } from "./i18n";

const ROLE_LABELS: Record<number, MessageKey> = {
  [Role.PERSONAL]: "format.role.personal",
  [Role.FRIENDS]: "format.role.friends",
};

const PLATFORM_LABELS: Record<number, string> = {
  [Platform.MACOS]: "macOS",
  [Platform.WINDOWS]: "Windows",
  [Platform.ANDROID]: "Android",
  [Platform.LINUX]: "Linux",
};

const PATH_LABELS: Record<number, MessageKey> = {
  [Path.LAN]: "format.path.lan",
  [Path.RELAY]: "format.path.relay",
};

/** roleLabel returns the name of a device role. */
export function roleLabel(role: Role): string {
  return translate(ROLE_LABELS[role] ?? "format.role.unknown");
}

/** platformLabel returns the name of the operating system of a device. */
export function platformLabel(platform: Platform): string {
  return PLATFORM_LABELS[platform] ?? translate("format.platform.unknown");
}

/** pathLabel returns the connection path to a device, or an empty string when
 * the device is offline. */
export function pathLabel(path: Path): string {
  const key = PATH_LABELS[path];
  return key ? translate(key) : "";
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
  return new Date(ms).toLocaleTimeString(getLocale(), {
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
  return new Date(ms).toLocaleString(getLocale(), {
    dateStyle: "medium",
    timeStyle: "short",
  });
}

const KIND_LABELS: Record<number, MessageKey> = {
  [Kind.TEXT]: "format.kind.text",
  [Kind.IMAGE]: "format.kind.image",
  [Kind.FILES]: "format.kind.files",
};

const TRANSFER_STATE_LABELS: Record<number, MessageKey> = {
  [TransferState.PENDING]: "format.transferState.pending",
  [TransferState.OFFERED]: "format.transferState.offered",
  [TransferState.TRANSFERRING]: "format.transferState.transferring",
  [TransferState.DONE]: "format.transferState.done",
  [TransferState.DECLINED]: "format.transferState.declined",
  [TransferState.CANCELED]: "format.transferState.canceled",
  [TransferState.FAILED]: "format.transferState.failed",
};

const TRANSFER_DIRECTION_LABELS: Record<number, MessageKey> = {
  [TransferDirection.OUTGOING]: "format.transferDirection.sent",
  [TransferDirection.INCOMING]: "format.transferDirection.received",
};

/** kindLabel returns the name of a clip kind. */
export function kindLabel(kind: Kind): string {
  return translate(KIND_LABELS[kind] ?? "format.kind.item");
}

/** transferStateLabel returns the name of a transfer state. */
export function transferStateLabel(state: TransferState): string {
  return translate(TRANSFER_STATE_LABELS[state] ?? "format.transferState.unknown");
}

/** transferDirectionLabel returns the direction of a transfer from the point
 * of view of this device. */
export function transferDirectionLabel(direction: TransferDirection): string {
  const key = TRANSFER_DIRECTION_LABELS[direction];
  return key ? translate(key) : "";
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

/** filesCountText names a number of files, with the plural of the locale. */
export function filesCountText(count: number): string {
  return translate("format.filesCount", { count });
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
      return snippet || translate("format.textItem");
    }
    case Kind.IMAGE:
      return translate("format.imageTitle", { size: formatBytes(itemSize(item)) });
    case Kind.FILES: {
      const names = item.representations.map((rep) => rep.fileName).filter((name) => name !== "");
      if (names.length === 0) {
        return translate("format.kind.files");
      }
      if (names.length === 1) {
        return names[0];
      }
      return translate("format.filesAndMore", {
        name: names[0],
        count: names.length - 1,
      });
    }
    default:
      return translate("format.kind.item");
  }
}

/** fileNames returns the file names of a history entry. */
export function fileNames(item: ClipItem | undefined): string[] {
  if (!item) {
    return [];
  }
  return item.representations.map((rep) => rep.fileName).filter((name) => name !== "");
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
  return filesCountText(transfer.files.length);
}
