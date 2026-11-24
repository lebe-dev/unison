import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  ClipItemSchema,
  FetchHistoryItemResponseSchema,
  HistoryItemSchema,
  Kind,
} from "./api/gen/unison/local/v1/local_pb";

const mocks = vi.hoisted(() => ({
  invoke: vi.fn(),
  fetchHistoryItem: vi.fn(),
}));

vi.mock("@tauri-apps/api/core", () => ({ invoke: mocks.invoke }));
vi.mock("./api/client", () => ({ client: { fetchHistoryItem: mocks.fetchHistoryItem } }));

import { canDragOut, crossedDragThreshold, dragItem, stageItem } from "./dragOut";

const MAC_UA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML)";
const originalUserAgent = navigator.userAgent;

function setUserAgent(userAgent: string): void {
  Object.defineProperty(navigator, "userAgent", { value: userAgent, configurable: true });
}

beforeEach(() => {
  vi.clearAllMocks();
  setUserAgent(originalUserAgent);
  mocks.fetchHistoryItem.mockResolvedValue(
    create(FetchHistoryItemResponseSchema, { files: ["/cache/drag/item-1/clip.png"] }),
  );
});

describe("drag threshold", () => {
  it("keeps a small move as a click", () => {
    expect(crossedDragThreshold({ x: 10, y: 10 }, { x: 11, y: 11 })).toBe(false);
  });

  it("starts a drag after a few pixels", () => {
    expect(crossedDragThreshold({ x: 10, y: 10 }, { x: 15, y: 10 })).toBe(true);
    expect(crossedDragThreshold({ x: 0, y: 0 }, { x: 3, y: 3 }, 4)).toBe(true);
  });
});

describe("drag out", () => {
  it("starts only on macOS", () => {
    setUserAgent(MAC_UA);
    expect(canDragOut()).toBe(true);
    setUserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64)");
    expect(canDragOut()).toBe(false);
  });

  it("stages the content of the item and drags the copies", async () => {
    mocks.invoke.mockResolvedValueOnce("/cache/drag/item-1-abc").mockResolvedValueOnce(undefined);

    await dragItem(imageHistory("item-1"));

    expect(mocks.invoke).toHaveBeenNthCalledWith(1, "drag_staging_dir", { itemId: "item-1" });
    expect(mocks.fetchHistoryItem).toHaveBeenCalledWith({
      id: "item-1",
      path: "/cache/drag/item-1-abc",
    });
    expect(mocks.invoke).toHaveBeenNthCalledWith(2, "start_file_drag", {
      paths: ["/cache/drag/item-1/clip.png"],
    });
  });

  it("returns the staged paths for a drag that waits", async () => {
    mocks.invoke.mockResolvedValueOnce("/cache/drag/item-1-abc");

    const paths = await stageItem(imageHistory("item-1"));

    expect(paths).toEqual(["/cache/drag/item-1/clip.png"]);
    expect(mocks.invoke).toHaveBeenCalledTimes(1);
  });

  it("refuses an item without content", async () => {
    mocks.invoke.mockResolvedValueOnce("/cache/drag/item-1-abc");
    mocks.fetchHistoryItem.mockResolvedValue(create(FetchHistoryItemResponseSchema, {}));

    await expect(stageItem(imageHistory("item-1"))).rejects.toThrow(
      "The item could not be dragged out.",
    );
  });
});

/** imageHistory returns a complete image item. */
function imageHistory(id: string) {
  return create(HistoryItemSchema, {
    complete: true,
    item: create(ClipItemSchema, {
      id,
      kind: Kind.IMAGE,
      representations: [{ mimeType: "image/png", size: 128n }],
    }),
  });
}
