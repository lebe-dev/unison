import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  ClipItemSchema,
  Kind,
  Path,
  Platform,
  Role,
  TransferDirection,
  TransferSchema,
  TransferState,
} from "./api/gen/unison/local/v1/local_pb";
import {
  formatBytes,
  formatTime,
  historyTitle,
  itemSize,
  kindLabel,
  pathLabel,
  platformLabel,
  progressPercent,
  roleLabel,
  shortDeviceID,
  textSnippet,
  transferDirectionLabel,
  transferStateLabel,
} from "./format";

describe("roleLabel", () => {
  it("names the roles", () => {
    expect(roleLabel(Role.PERSONAL)).toBe("Personal");
    expect(roleLabel(Role.FRIENDS)).toBe("Friends");
    expect(roleLabel(Role.UNSPECIFIED)).toBe("Unknown role");
  });
});

describe("platformLabel", () => {
  it("names the systems", () => {
    expect(platformLabel(Platform.MACOS)).toBe("macOS");
    expect(platformLabel(Platform.WINDOWS)).toBe("Windows");
    expect(platformLabel(Platform.ANDROID)).toBe("Android");
    expect(platformLabel(Platform.LINUX)).toBe("Linux");
    expect(platformLabel(Platform.UNSPECIFIED)).toBe("Unknown system");
  });
});

describe("pathLabel", () => {
  it("names the connection paths", () => {
    expect(pathLabel(Path.LAN)).toBe("LAN");
    expect(pathLabel(Path.RELAY)).toBe("Relay");
    expect(pathLabel(Path.UNSPECIFIED)).toBe("");
  });
});

describe("shortDeviceID", () => {
  it("groups the first twelve characters", () => {
    expect(shortDeviceID("0123456789abcdef")).toBe("0123 4567 89AB");
    expect(shortDeviceID("abc")).toBe("ABC");
    expect(shortDeviceID("")).toBe("");
  });
});

describe("formatTime", () => {
  it("keeps an absent timestamp empty", () => {
    expect(formatTime(0)).toBe("");
    expect(formatTime(0n)).toBe("");
  });
});

describe("formatBytes", () => {
  it("uses the next unit from a kibibyte", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1023)).toBe("1023 B");
    expect(formatBytes(1024)).toBe("1.0 KB");
    expect(formatBytes(2 * 1024 * 1024 * 1024)).toBe("2.0 GB");
    expect(formatBytes(-1)).toBe("");
  });
});

describe("kindLabel", () => {
  it("names the clip kinds", () => {
    expect(kindLabel(Kind.TEXT)).toBe("Text");
    expect(kindLabel(Kind.IMAGE)).toBe("Image");
    expect(kindLabel(Kind.FILES)).toBe("Files");
    expect(kindLabel(Kind.UNSPECIFIED)).toBe("Item");
  });
});

describe("textSnippet", () => {
  it("keeps the inline text on one line and cuts it", () => {
    const item = textItem("hello\nworld");

    expect(textSnippet(item)).toBe("hello world");
    expect(textSnippet(item, 7)).toBe("hello...");
  });
});

describe("historyTitle", () => {
  it("describes a text item by its snippet", () => {
    expect(historyTitle(textItem("a note"))).toBe("a note");
  });

  it("describes an image by its size", () => {
    const item = create(ClipItemSchema, {
      kind: Kind.IMAGE,
      representations: [{ mimeType: "image/png", size: 2048n, inlineBytes: new Uint8Array([1]) }],
    });

    expect(historyTitle(item)).toBe("Image - 2.0 KB");
    expect(itemSize(item)).toBe(2048);
  });

  it("describes files by their names", () => {
    const item = create(ClipItemSchema, {
      kind: Kind.FILES,
      representations: [
        { mimeType: "application/x-unison-file", fileName: "report.pdf", size: 10n },
        { mimeType: "application/x-unison-file", fileName: "photo.jpg", size: 20n },
      ],
    });

    expect(historyTitle(item)).toBe("report.pdf and 1 more");
  });
});

describe("progressPercent", () => {
  it("shares the transferred bytes with the size", () => {
    expect(progressPercent(transfer({ bytes: 50n, totalBytes: 200n }))).toBe(25);
    expect(progressPercent(transfer({ bytes: 0n, totalBytes: 0n }))).toBe(0);
    expect(progressPercent(transfer({ bytes: 300n, totalBytes: 200n }))).toBe(100);
  });
});

describe("transfer labels", () => {
  it("names the states and the directions", () => {
    expect(transferStateLabel(TransferState.PENDING)).toBe("Waiting for an answer");
    expect(transferStateLabel(TransferState.TRANSFERRING)).toBe("Transferring");
    expect(transferStateLabel(TransferState.DONE)).toBe("Done");
    expect(transferDirectionLabel(TransferDirection.INCOMING)).toBe("Received");
    expect(transferDirectionLabel(TransferDirection.OUTGOING)).toBe("Sent");
  });
});

function textItem(text: string) {
  return create(ClipItemSchema, {
    kind: Kind.TEXT,
    representations: [
      {
        mimeType: "text/plain",
        size: BigInt(text.length),
        inlineBytes: new TextEncoder().encode(text),
      },
    ],
  });
}

function transfer(fields: { bytes: bigint; totalBytes: bigint }) {
  return create(TransferSchema, {
    id: "t1",
    direction: TransferDirection.INCOMING,
    state: TransferState.TRANSFERRING,
    bytes: fields.bytes,
    totalBytes: fields.totalBytes,
  });
}
