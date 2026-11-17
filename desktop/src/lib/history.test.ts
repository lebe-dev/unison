import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  ApplyHistoryItemResponseSchema,
  ClipItemSchema,
  CopyHistoryItemResponseSchema,
  DeleteHistoryItemResponseSchema,
  EventKind,
  EventSchema,
  FetchHistoryItemResponseSchema,
  HistoryItemSchema,
  Kind,
  ListHistoryResponseSchema,
} from "./api/gen/unison/local/v1/local_pb";
import { client } from "./api/client";
import { history } from "./history.svelte";
import { updateTray } from "./tray";

vi.mock("./api/client", () => ({
  client: {
    listHistory: vi.fn(),
    getSettings: vi.fn(),
    copyHistoryItem: vi.fn(),
    applyHistoryItem: vi.fn(),
    fetchHistoryItem: vi.fn(),
    deleteHistoryItem: vi.fn(),
  },
}));

vi.mock("./tray", () => ({
  updateTray: vi.fn(),
}));

const eventHarness = vi.hoisted(() => {
  const handlers: Array<(event: unknown) => void> = [];
  return {
    handlers,
    onEvent(handler: (event: unknown) => void) {
      handlers.push(handler);
      return () => {
        handlers.length = 0;
      };
    },
  };
});

vi.mock("./daemon.svelte", () => ({
  daemon: { onEvent: eventHarness.onEvent },
}));

const listHistory = vi.mocked(client.listHistory);
const getSettings = vi.mocked(client.getSettings);
const copyHistoryItem = vi.mocked(client.copyHistoryItem);
const applyHistoryItem = vi.mocked(client.applyHistoryItem);
const fetchHistoryItem = vi.mocked(client.fetchHistoryItem);
const deleteHistoryItem = vi.mocked(client.deleteHistoryItem);
const updateTrayMock = vi.mocked(updateTray);

beforeEach(() => {
  vi.clearAllMocks();
  updateTrayMock.mockResolvedValue(undefined);
  history.items = [];
  history.kind = Kind.UNSPECIFIED;
  history.origin = "";
  history.error = null;
  history.notice = null;
  history.downloadsDir = "";
  history.downloading = [];
  history.held = [];
});

describe("history list", () => {
  it("loads the list with the current filter", async () => {
    listHistory.mockResolvedValue(
      create(ListHistoryResponseSchema, { items: [textHistory("a", "first")] }),
    );

    await history.load();

    expect(listHistory).toHaveBeenCalledWith(
      expect.objectContaining({ kind: Kind.UNSPECIFIED, deviceId: "", limit: 200 }),
    );
    expect(history.items).toHaveLength(1);
    expect(history.error).toBeNull();
  });

  it("applies a kind filter and reloads", async () => {
    listHistory.mockResolvedValue(create(ListHistoryResponseSchema, {}));

    await history.setKind(Kind.IMAGE);

    expect(history.kind).toBe(Kind.IMAGE);
    expect(listHistory).toHaveBeenLastCalledWith(expect.objectContaining({ kind: Kind.IMAGE }));
  });

  it("applies a device filter and reloads", async () => {
    listHistory.mockResolvedValue(create(ListHistoryResponseSchema, {}));

    await history.setOrigin("device-1");

    expect(history.origin).toBe("device-1");
    expect(listHistory).toHaveBeenLastCalledWith(expect.objectContaining({ deviceId: "device-1" }));
  });

  it("reports a failure of the daemon", async () => {
    listHistory.mockRejectedValue(new Error("connection refused"));

    await history.load();

    expect(history.error).toBe("connection refused");
  });
});

describe("history actions", () => {
  it("copies an item back to the clipboard", async () => {
    copyHistoryItem.mockResolvedValue(create(CopyHistoryItemResponseSchema, {}));

    await history.copy("item-1");

    expect(copyHistoryItem).toHaveBeenCalledWith(expect.objectContaining({ id: "item-1" }));
    expect(history.notice?.kind).toBe("info");
  });

  it("downloads a file into the downloads directory", async () => {
    history.downloadsDir = "/downloads";
    fetchHistoryItem.mockResolvedValue(
      create(FetchHistoryItemResponseSchema, {
        item: fileHistory("item-1", "report.pdf"),
        files: ["/downloads/report.pdf"],
      }),
    );
    history.items = [fileHistory("item-1", "report.pdf")];

    await history.download(history.items[0]);

    expect(fetchHistoryItem).toHaveBeenCalledWith(
      expect.objectContaining({ id: "item-1", path: "/downloads" }),
    );
    expect(history.notice?.text).toContain("/downloads/report.pdf");
    expect(history.downloading).toEqual([]);
  });

  it("downloads an image into the local store", async () => {
    fetchHistoryItem.mockResolvedValue(
      create(FetchHistoryItemResponseSchema, { item: textHistory("item-2", "text") }),
    );

    await history.download(textHistory("item-2", "text"));

    expect(fetchHistoryItem).toHaveBeenCalledWith(
      expect.objectContaining({ id: "item-2", path: "" }),
    );
    expect(history.notice?.text).toBe("Downloaded to the local store.");
  });

  it("removes a deleted item from the list", async () => {
    deleteHistoryItem.mockResolvedValue(create(DeleteHistoryItemResponseSchema, {}));
    listHistory.mockResolvedValue(create(ListHistoryResponseSchema, {}));
    history.items = [textHistory("item-1", "first"), textHistory("item-2", "second")];

    await history.remove("item-1");

    expect(deleteHistoryItem).toHaveBeenCalledWith(expect.objectContaining({ id: "item-1" }));
    expect(history.items.map((item) => item.item?.id)).toEqual(["item-2"]);
  });

  it("keeps an error of a copy", async () => {
    copyHistoryItem.mockRejectedValue(new Error("unsupported content"));

    await history.copy("item-1");

    expect(history.notice).toEqual({ kind: "error", text: "unsupported content" });
  });

  it("applies an item that waited for confirmation", async () => {
    applyHistoryItem.mockResolvedValue(create(ApplyHistoryItemResponseSchema, {}));
    history.held = ["item-1", "item-2"];

    await history.apply("item-1");

    expect(applyHistoryItem).toHaveBeenCalledWith(expect.objectContaining({ id: "item-1" }));
    expect(history.held).toEqual(["item-2"]);
    expect(history.notice?.kind).toBe("info");
  });

  it("keeps an item that could not be applied", async () => {
    applyHistoryItem.mockRejectedValue(new Error("the item is gone"));
    history.held = ["item-1"];

    await history.apply("item-1");

    expect(history.held).toEqual(["item-1"]);
    expect(history.notice).toEqual({ kind: "error", text: "the item is gone" });
  });
});

describe("held items", () => {
  it("collects the items that wait for confirmation", () => {
    listHistory.mockResolvedValue(create(ListHistoryResponseSchema, {}));
    history.start();

    eventHarness.handlers[0](
      create(EventSchema, {
        kind: EventKind.CLIP_APPLY,
        item: create(ClipItemSchema, { id: "item-1", kind: Kind.TEXT }),
      }),
    );

    expect(history.held).toEqual(["item-1"]);
    history.stop();
  });
});

describe("tray entries", () => {
  it("keeps the five newest items", async () => {
    const items = [1, 2, 3, 4, 5, 6].map((index) => textHistory(`item-${index}`, `text ${index}`));
    listHistory.mockResolvedValue(create(ListHistoryResponseSchema, { items }));

    await history.refreshRecent();

    expect(listHistory).toHaveBeenCalledWith(expect.objectContaining({ limit: 5 }));
    const recent = updateTrayMock.mock.calls.at(-1)?.[0]?.recent ?? [];
    expect(recent).toHaveLength(5);
    expect(recent[0]).toEqual({ id: "item-1", label: "text 1" });
  });

  it("labels files with their kind", async () => {
    listHistory.mockResolvedValue(
      create(ListHistoryResponseSchema, { items: [fileHistory("item-1", "report.pdf")] }),
    );

    await history.refreshRecent();

    const recent = updateTrayMock.mock.calls.at(-1)?.[0]?.recent ?? [];
    expect(recent[0].label).toBe("Files: report.pdf");
  });

  it("reads the downloads directory", async () => {
    getSettings.mockResolvedValue({
      $typeName: "unison.local.v1.GetSettingsResponse",
      settings: {
        $typeName: "unison.local.v1.Settings",
        deviceName: "Mac",
        requireConfirmation: false,
        requireFileConfirmation: false,
        blobMaxSizeBytes: 0n,
        downloadsDir: "/downloads",
        relayMaxSizeBytes: 0n,
        historyTtlTextSeconds: 0n,
        historyTtlImageSeconds: 0n,
        historyTtlFilesSeconds: 0n,
        sentryEnabled: false,
        stripTracking: true,
        journalEnabled: false,
      },
    });

    await history.loadSettings();

    expect(history.downloadsDir).toBe("/downloads");
  });
});

function textHistory(id: string, text: string) {
  return create(HistoryItemSchema, {
    complete: true,
    item: create(ClipItemSchema, {
      id,
      kind: Kind.TEXT,
      representations: [
        {
          mimeType: "text/plain",
          size: BigInt(text.length),
          inlineBytes: new TextEncoder().encode(text),
        },
      ],
    }),
  });
}

function fileHistory(id: string, name: string) {
  return create(HistoryItemSchema, {
    complete: false,
    item: create(ClipItemSchema, {
      id,
      kind: Kind.FILES,
      representations: [
        { mimeType: "application/x-unison-file", fileName: name, size: 2048n, blake3: "ab" },
      ],
    }),
  });
}
