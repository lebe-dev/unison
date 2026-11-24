import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen } from "@testing-library/svelte";
import { tick } from "svelte";
import {
  ClipItemSchema,
  CopyHistoryItemResponseSchema,
  DeleteHistoryItemResponseSchema,
  GetSettingsResponseSchema,
  HistoryItemSchema,
  Kind,
  ListHistoryResponseSchema,
} from "../api/gen/unison/local/v1/local_pb";

const mocks = vi.hoisted(() => {
  const transfersNotice: { kind: "info" | "error"; text: string } | null = {
    kind: "error",
    text: "The transfer failed.",
  };
  return {
    listHistory: vi.fn(),
    getSettings: vi.fn(),
    copyHistoryItem: vi.fn(),
    fetchHistoryItem: vi.fn(),
    deleteHistoryItem: vi.fn(),
    stageItem: vi.fn(),
    startStagedDrag: vi.fn(),
    transfersSend: vi.fn(),
    transfersNotice,
    daemon: {
      connected: true,
      loading: false,
      retry: vi.fn(),
      devices: [] as Array<{ deviceId: string; name: string; online: boolean; revoked: boolean }>,
      onEvent: vi.fn(() => () => {}),
    },
  };
});

vi.mock("../api/client", () => ({
  client: {
    listHistory: mocks.listHistory,
    getSettings: mocks.getSettings,
    copyHistoryItem: mocks.copyHistoryItem,
    fetchHistoryItem: mocks.fetchHistoryItem,
    deleteHistoryItem: mocks.deleteHistoryItem,
  },
}));

vi.mock("../daemon.svelte", () => ({ daemon: mocks.daemon }));

vi.mock("../transfers.svelte", () => ({
  transfers: {
    send: mocks.transfersSend,
    get notice() {
      return mocks.transfersNotice;
    },
  },
}));

vi.mock("../thumbnails", () => ({
  loadThumbnail: vi.fn().mockResolvedValue(null),
}));

vi.mock("../dragOut", () => ({
  canDragOut: () => true,
  crossedDragThreshold: (start: { x: number; y: number }, current: { x: number; y: number }) =>
    Math.hypot(current.x - start.x, current.y - start.y) >= 4,
  stageItem: mocks.stageItem,
  startStagedDrag: mocks.startStagedDrag,
}));

import { dashboard, DEFAULT_DASHBOARD_LIMIT } from "../dashboard.svelte";
import { updater } from "../updater.svelte";
import type { UpdaterSnapshot } from "../updater";
import Dashboard from "./Dashboard.svelte";

beforeEach(() => {
  vi.clearAllMocks();
  mocks.daemon.connected = true;
  mocks.daemon.loading = false;
  mocks.daemon.devices = [];
  mocks.transfersNotice = { kind: "error", text: "The transfer failed." };
  mocks.deleteHistoryItem.mockResolvedValue(create(DeleteHistoryItemResponseSchema, {}));
  dashboard.stop();
  dashboard.limit = DEFAULT_DASHBOARD_LIMIT;
  dashboard.items = { images: [], files: [], text: [] };
  dashboard.loading = [];
  dashboard.error = null;
  dashboard.notice = null;
  dashboard.downloadsDir = "";
  updater.snapshot = null;
  updater.dialogOpen = false;
  mocks.getSettings.mockResolvedValue(create(GetSettingsResponseSchema, {}));
  mocks.copyHistoryItem.mockResolvedValue(create(CopyHistoryItemResponseSchema, {}));
  mocks.listHistory.mockImplementation((request: { kind: Kind }) => {
    switch (request.kind) {
      case Kind.IMAGE:
        return Promise.resolve(
          create(ListHistoryResponseSchema, { items: [imageHistory("image-1")] }),
        );
      case Kind.FILES:
        return Promise.resolve(
          create(ListHistoryResponseSchema, { items: [fileHistory("files-1")] }),
        );
      default:
        return Promise.resolve(create(ListHistoryResponseSchema, {}));
    }
  });
});

afterEach(() => {
  dashboard.stop();
});

describe("dashboard tabs", () => {
  it("starts on the images of the newest items", async () => {
    render(Dashboard);

    expect(await screen.findByRole("button", { name: "Image - 128 B" })).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Images" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByRole("tab", { name: "Files" }).getAttribute("aria-selected")).toBe("false");
  });

  it("switches to the files of the newest items", async () => {
    render(Dashboard);
    await screen.findByRole("button", { name: "Image - 128 B" });

    await fireEvent.click(screen.getByRole("tab", { name: "Files" }));

    expect(screen.getByRole("tab", { name: "Files" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByRole("button", { name: /report\.pdf/ })).toBeTruthy();
    expect(screen.getAllByText("report.pdf")).toHaveLength(1);
    expect(screen.getByText("2.0 KB")).toBeTruthy();
    expect(screen.getByText("Not downloaded")).toBeTruthy();
  });

  it("runs copy, download and actions menu directly from a file row", async () => {
    mocks.fetchHistoryItem.mockResolvedValue({ files: ["/tmp/report.pdf"] });
    render(Dashboard);
    await screen.findByRole("button", { name: "Image - 128 B" });
    await fireEvent.click(screen.getByRole("tab", { name: "Files" }));

    await fireEvent.click(screen.getByRole("button", { name: "Copy" }));
    expect(mocks.copyHistoryItem).toHaveBeenCalledWith({ id: "files-1" });

    await fireEvent.click(screen.getByRole("button", { name: "Download" }));
    expect(mocks.fetchHistoryItem).toHaveBeenCalledWith({ id: "files-1", path: "" });

    await fireEvent.click(screen.getByRole("button", { name: "Item actions" }));
    expect(screen.getByRole("menu")).toBeTruthy();
  });

  it("shows origin device and file list for multi-file items", async () => {
    mocks.daemon.devices = [{ deviceId: "peer-1", name: "Pixel 9", online: true, revoked: false }];
    mocks.listHistory.mockImplementation((request: { kind: Kind }) => {
      if (request.kind === Kind.FILES) {
        return Promise.resolve(
          create(ListHistoryResponseSchema, {
            items: [
              create(HistoryItemSchema, {
                complete: true,
                item: create(ClipItemSchema, {
                  id: "files-multi",
                  kind: Kind.FILES,
                  origin: "peer-1",
                  createdAtUnixMs: 1_700_000_000_000n,
                  representations: [
                    { mimeType: "application/x-unison-file", fileName: "a.jpg", size: 1024n },
                    { mimeType: "application/x-unison-file", fileName: "b.pdf", size: 2048n },
                  ],
                }),
              }),
            ],
          }),
        );
      }
      return Promise.resolve(create(ListHistoryResponseSchema, {}));
    });

    render(Dashboard);
    await fireEvent.click(await screen.findByRole("tab", { name: "Files" }));

    expect(await screen.findByText("a.jpg and 1 more")).toBeTruthy();
    expect(screen.getByText("2 files")).toBeTruthy();
    expect(screen.getByText("3.0 KB")).toBeTruthy();
    expect(screen.getByText("Pixel 9")).toBeTruthy();
    expect(screen.getByText("a.jpg, b.pdf")).toBeTruthy();
  });

  it("moves between the tabs with the arrow keys", async () => {
    render(Dashboard);
    const images = await screen.findByRole("tab", { name: "Images" });

    await fireEvent.keyDown(images, { key: "ArrowRight" });

    expect(screen.getByRole("tab", { name: "Files" }).getAttribute("aria-selected")).toBe("true");
  });

  it("keeps the tabs empty when the kind has no items", async () => {
    render(Dashboard);
    await screen.findByRole("button", { name: "Image - 128 B" });

    await fireEvent.click(screen.getByRole("tab", { name: "Text" }));

    expect(screen.getByText("No text yet.")).toBeTruthy();
  });

  it("reports a failure of the daemon", async () => {
    mocks.listHistory.mockRejectedValue(new Error("connection refused"));

    render(Dashboard);

    expect(await screen.findByText("connection refused")).toBeTruthy();
  });

  it("shows the starting state without a daemon", () => {
    mocks.daemon.connected = false;
    mocks.daemon.loading = true;

    render(Dashboard);

    expect(screen.getByText("Connecting to the daemon...")).toBeTruthy();
  });
});

describe("dashboard preview", () => {
  it("opens an image in the preview", async () => {
    render(Dashboard);

    await fireEvent.click(await screen.findByRole("button", { name: "Image - 128 B" }));

    expect(screen.getByRole("dialog", { name: "Item preview" })).toBeTruthy();

    await fireEvent.click(screen.getByRole("button", { name: "Close" }));

    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("copies the text of an item from the preview", async () => {
    mocks.listHistory.mockImplementation(() =>
      Promise.resolve(
        create(ListHistoryResponseSchema, { items: [textHistory("text-1", "hello world")] }),
      ),
    );
    render(Dashboard);
    await fireEvent.click(await screen.findByRole("tab", { name: "Text" }));

    await fireEvent.click(await screen.findByRole("button", { name: /hello world/ }));
    await fireEvent.click(screen.getByRole("button", { name: "Copy" }));

    expect(mocks.copyHistoryItem).toHaveBeenCalledWith({ id: "text-1" });
  });

  it("does not open the preview when the press became a drag", async () => {
    mocks.stageItem.mockResolvedValue(["/staging/clip.png"]);
    mocks.startStagedDrag.mockResolvedValue(undefined);
    render(Dashboard);
    const tile = await screen.findByRole("button", { name: "Image - 128 B" });

    await fireEvent.pointerDown(tile, { button: 0, clientX: 10, clientY: 10, pointerId: 1 });
    await fireEvent.pointerMove(tile, { clientX: 30, clientY: 10, pointerId: 1 });
    await vi.waitFor(() =>
      expect(mocks.startStagedDrag).toHaveBeenCalledWith(["/staging/clip.png"]),
    );
    await fireEvent.pointerUp(tile, { pointerId: 1 });
    await fireEvent.click(tile, { detail: 1 });
    await tick();

    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("stages nothing for a plain click", async () => {
    render(Dashboard);

    await fireEvent.click(await screen.findByRole("button", { name: "Image - 128 B" }));

    expect(screen.getByRole("dialog", { name: "Item preview" })).toBeTruthy();
    expect(mocks.stageItem).not.toHaveBeenCalled();
  });
});

describe("dashboard menu", () => {
  it("shows the actions of an item on a right click", async () => {
    render(Dashboard);
    const tile = await screen.findByRole("button", { name: "Image - 128 B" });

    await fireEvent.contextMenu(tile);

    expect(screen.getByRole("menu")).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: "Send" })).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: "Copy" })).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: "Delete" })).toBeTruthy();
  });

  it("closes the menu with Escape", async () => {
    render(Dashboard);
    const tile = await screen.findByRole("button", { name: "Image - 128 B" });
    await fireEvent.contextMenu(tile);

    await fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });

    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("sends the item to the device the user picks", async () => {
    mocks.daemon.devices = [{ deviceId: "device-1", name: "Laptop", online: true, revoked: false }];
    mocks.stageItem.mockResolvedValue(["/staging/clip.png"]);
    mocks.transfersSend.mockResolvedValue(true);
    render(Dashboard);

    await fireEvent.contextMenu(await screen.findByRole("button", { name: "Image - 128 B" }));
    await fireEvent.click(screen.getByRole("menuitem", { name: "Send" }));
    await fireEvent.click(await screen.findByRole("menuitem", { name: "Laptop" }));

    expect(mocks.stageItem).toHaveBeenCalled();
    expect(mocks.transfersSend).toHaveBeenCalledWith(["/staging/clip.png"], ["device-1"]);
    expect(await screen.findByText("Sent to Laptop.")).toBeTruthy();
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("says so when no device is online", async () => {
    render(Dashboard);

    await fireEvent.contextMenu(await screen.findByRole("button", { name: "Image - 128 B" }));
    await fireEvent.click(screen.getByRole("menuitem", { name: "Send" }));

    expect(screen.getByText("No device is online right now.")).toBeTruthy();
  });

  it("copies an image through the daemon without sending it", async () => {
    render(Dashboard);

    await fireEvent.contextMenu(await screen.findByRole("button", { name: "Image - 128 B" }));
    await fireEvent.click(screen.getByRole("menuitem", { name: "Copy" }));

    expect(mocks.copyHistoryItem).toHaveBeenCalledWith({ id: "image-1" });
    expect(mocks.transfersSend).not.toHaveBeenCalled();
    expect(await screen.findByText("The item is on the clipboard.")).toBeTruthy();
  });

  it("copies text through the daemon", async () => {
    mocks.listHistory.mockImplementation(() =>
      Promise.resolve(
        create(ListHistoryResponseSchema, { items: [textHistory("text-1", "hello world")] }),
      ),
    );
    render(Dashboard);
    await fireEvent.click(await screen.findByRole("tab", { name: "Text" }));

    await fireEvent.contextMenu(await screen.findByRole("button", { name: /hello world/ }));
    await fireEvent.click(screen.getByRole("menuitem", { name: "Copy" }));

    expect(mocks.copyHistoryItem).toHaveBeenCalledWith({ id: "text-1" });
    expect(mocks.stageItem).not.toHaveBeenCalled();
  });

  it("asks before it deletes the item", async () => {
    render(Dashboard);

    await fireEvent.contextMenu(await screen.findByRole("button", { name: "Image - 128 B" }));
    await fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));

    expect(screen.getByText("Delete the item?")).toBeTruthy();
    expect(mocks.deleteHistoryItem).not.toHaveBeenCalled();

    await fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));

    expect(mocks.deleteHistoryItem).toHaveBeenCalledWith({ id: "image-1" });
    expect(await screen.findByText("The item was deleted.")).toBeTruthy();
    expect(screen.queryByRole("menu")).toBeNull();
  });
});

/** imageHistory returns a complete image item. */
function imageHistory(id: string) {
  return create(HistoryItemSchema, {
    complete: true,
    item: create(ClipItemSchema, {
      id,
      kind: Kind.IMAGE,
      createdAtUnixMs: 1n,
      representations: [{ mimeType: "image/png", size: 128n }],
    }),
  });
}

/** fileHistory returns a file item with one file. */
function fileHistory(id: string) {
  return create(HistoryItemSchema, {
    complete: false,
    item: create(ClipItemSchema, {
      id,
      kind: Kind.FILES,
      createdAtUnixMs: 1n,
      representations: [
        { mimeType: "application/x-unison-file", fileName: "report.pdf", size: 2048n },
      ],
    }),
  });
}

/** textHistory returns a complete text item. */
function textHistory(id: string, text: string) {
  return create(HistoryItemSchema, {
    complete: true,
    item: create(ClipItemSchema, {
      id,
      kind: Kind.TEXT,
      createdAtUnixMs: 1n,
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

/** updaterSnapshot returns a state with one available release. */
function updaterSnapshot(overrides: Partial<UpdaterSnapshot> = {}): UpdaterSnapshot {
  return {
    currentVersion: "0.2.5",
    supported: true,
    unsupportedReason: null,
    config: {
      enabled: false,
      betaChannel: false,
      interval: "daily",
      lastCheckedUnixMs: null,
      dismissedVersion: null,
      lastEtag: null,
    },
    status: { kind: "available" },
    available: {
      version: "0.3.0",
      name: "Unison 0.3.0",
      body: "## What is new",
      prerelease: false,
      publishedAt: "2026-11-24T10:00:00Z",
      htmlUrl: "https://github.com/lebe-dev/unison/releases/tag/0.3.0",
      assetSize: 1024,
    },
    dismissed: false,
    ...overrides,
  };
}

describe("dashboard update banner", () => {
  afterEach(() => {
    updater.snapshot = null;
  });

  it("shows an available update even without a daemon", () => {
    mocks.daemon.connected = false;
    mocks.daemon.loading = false;
    updater.snapshot = updaterSnapshot();

    render(Dashboard);

    expect(screen.getByText("Unison 0.3.0 is available")).toBeTruthy();
    expect(screen.getByText(/The daemon is not running/)).toBeTruthy();
  });

  it("hides the banner of a release the user put aside", () => {
    updater.snapshot = updaterSnapshot({ dismissed: true });

    render(Dashboard);

    expect(screen.queryByText("Unison 0.3.0 is available")).toBeNull();
  });
});
