import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen } from "@testing-library/svelte";
import { tick } from "svelte";
import {
  ClipItemSchema,
  CopyHistoryItemResponseSchema,
  GetSettingsResponseSchema,
  HistoryItemSchema,
  Kind,
  ListHistoryResponseSchema,
} from "../api/gen/unison/local/v1/local_pb";

const mocks = vi.hoisted(() => ({
  listHistory: vi.fn(),
  getSettings: vi.fn(),
  copyHistoryItem: vi.fn(),
  fetchHistoryItem: vi.fn(),
  stageItem: vi.fn(),
  startStagedDrag: vi.fn(),
  daemon: {
    connected: true,
    loading: false,
    retry: vi.fn(),
    onEvent: vi.fn(() => () => {}),
  },
}));

vi.mock("../api/client", () => ({
  client: {
    listHistory: mocks.listHistory,
    getSettings: mocks.getSettings,
    copyHistoryItem: mocks.copyHistoryItem,
    fetchHistoryItem: mocks.fetchHistoryItem,
  },
}));

vi.mock("../daemon.svelte", () => ({ daemon: mocks.daemon }));

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
import Dashboard from "./Dashboard.svelte";

beforeEach(() => {
  vi.clearAllMocks();
  mocks.daemon.connected = true;
  mocks.daemon.loading = false;
  dashboard.stop();
  dashboard.limit = DEFAULT_DASHBOARD_LIMIT;
  dashboard.items = { images: [], files: [], text: [] };
  dashboard.loading = [];
  dashboard.error = null;
  dashboard.notice = null;
  dashboard.downloadsDir = "";
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
