import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  ClipItemSchema,
  CopyHistoryItemResponseSchema,
  EventKind,
  EventSchema,
  FetchHistoryItemResponseSchema,
  GetSettingsResponseSchema,
  HistoryItemSchema,
  Kind,
  ListHistoryResponseSchema,
} from "./api/gen/unison/local/v1/local_pb";

const mocks = vi.hoisted(() => {
  const handlers: Array<(event: unknown) => void> = [];
  return {
    handlers,
    listHistory: vi.fn(),
    getSettings: vi.fn(),
    copyHistoryItem: vi.fn(),
    fetchHistoryItem: vi.fn(),
    onEvent(handler: (event: unknown) => void) {
      handlers.push(handler);
      return () => {
        handlers.length = 0;
      };
    },
  };
});

vi.mock("./api/client", () => ({
  client: {
    listHistory: mocks.listHistory,
    getSettings: mocks.getSettings,
    copyHistoryItem: mocks.copyHistoryItem,
    fetchHistoryItem: mocks.fetchHistoryItem,
  },
}));

vi.mock("./daemon.svelte", () => ({
  daemon: { onEvent: mocks.onEvent },
}));

import {
  DASHBOARD_LIMIT_OPTIONS,
  DEFAULT_DASHBOARD_LIMIT,
  dashboard,
  dashboardLimitFrom,
} from "./dashboard.svelte";

beforeEach(() => {
  vi.clearAllMocks();
  dashboard.stop();
  localStorage.clear();
  dashboard.limit = DEFAULT_DASHBOARD_LIMIT;
  dashboard.items = { images: [], files: [], text: [] };
  dashboard.loading = [];
  dashboard.error = null;
  dashboard.notice = null;
  dashboard.downloadsDir = "";
  mocks.handlers.length = 0;
  mocks.getSettings.mockResolvedValue(create(GetSettingsResponseSchema, {}));
  mocks.listHistory.mockResolvedValue(create(ListHistoryResponseSchema, {}));
});

describe("dashboard preferences", () => {
  it("keeps 12 items by default", () => {
    expect(dashboard.limit).toBe(12);
    expect(DEFAULT_DASHBOARD_LIMIT).toBe(12);
    expect(DASHBOARD_LIMIT_OPTIONS).toEqual([6, 12, 24, 48]);
  });

  it("stores the number of items", () => {
    dashboard.setLimit(24);

    expect(dashboard.limit).toBe(24);
    expect(localStorage.getItem("unison.dashboard.limit")).toBe("24");
  });

  it("falls back to the default when the stored number is not offered", () => {
    expect(dashboardLimitFrom("24")).toBe(24);
    for (const value of ["7", "0", "", "abc", null]) {
      expect(dashboardLimitFrom(value)).toBe(12);
    }
  });
});

describe("dashboard lists", () => {
  it("asks for the newest items of one kind", async () => {
    dashboard.setLimit(6);
    mocks.listHistory.mockResolvedValue(
      create(ListHistoryResponseSchema, { items: [imageHistory("a")] }),
    );

    await dashboard.load("images");

    expect(mocks.listHistory).toHaveBeenCalledWith(
      expect.objectContaining({ kind: Kind.IMAGE, limit: 6 }),
    );
    expect(dashboard.items.images).toHaveLength(1);
    expect(dashboard.error).toBeNull();
  });

  it("asks for every kind of the tabs", async () => {
    await dashboard.loadAll();

    for (const kind of [Kind.IMAGE, Kind.FILES, Kind.TEXT]) {
      expect(mocks.listHistory).toHaveBeenCalledWith(expect.objectContaining({ kind }));
    }
  });

  it("keeps only the configured number of items", async () => {
    dashboard.setLimit(6);
    mocks.listHistory.mockResolvedValue(
      create(ListHistoryResponseSchema, {
        items: Array.from({ length: 10 }, (_, index) => textHistory(`id-${index}`)),
      }),
    );

    await dashboard.load("text");

    expect(dashboard.items.text).toHaveLength(6);
  });

  it("reports a failure of the daemon", async () => {
    mocks.listHistory.mockRejectedValue(new Error("connection refused"));

    await dashboard.load("files");

    expect(dashboard.error).toBe("connection refused");
  });

  it("reloads the tabs after an event of the daemon", async () => {
    dashboard.start();
    await dashboard.loadAll();
    expect(mocks.listHistory).toHaveBeenCalledTimes(3);

    mocks.handlers.at(-1)?.(create(EventSchema, { kind: EventKind.CLIPBOARD }));

    await vi.waitFor(() => expect(mocks.listHistory).toHaveBeenCalledTimes(6));
    dashboard.stop();
  });
});

describe("dashboard actions", () => {
  it("copies an item back to the clipboard", async () => {
    mocks.copyHistoryItem.mockResolvedValue(create(CopyHistoryItemResponseSchema, {}));

    await dashboard.copy("a");

    expect(mocks.copyHistoryItem).toHaveBeenCalledWith({ id: "a" });
    expect(dashboard.notice?.kind).toBe("info");
  });

  it("downloads the files into the downloads folder", async () => {
    dashboard.downloadsDir = "/downloads";
    mocks.fetchHistoryItem.mockResolvedValue(
      create(FetchHistoryItemResponseSchema, { files: ["/downloads/clip.png"] }),
    );

    await dashboard.download("a");

    expect(mocks.fetchHistoryItem).toHaveBeenCalledWith({ id: "a", path: "/downloads" });
    expect(dashboard.notice?.text).toContain("/downloads/clip.png");
  });

  it("reports a failed download", async () => {
    mocks.fetchHistoryItem.mockRejectedValue(new Error("the device is offline"));

    await dashboard.download("a");

    expect(dashboard.notice?.kind).toBe("error");
    expect(dashboard.notice?.text).toBe("the device is offline");
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

/** textHistory returns a complete text item. */
function textHistory(id: string) {
  return create(HistoryItemSchema, {
    complete: true,
    item: create(ClipItemSchema, {
      id,
      kind: Kind.TEXT,
      createdAtUnixMs: 1n,
      representations: [
        { mimeType: "text/plain", size: 4n, inlineBytes: new TextEncoder().encode("text") },
      ],
    }),
  });
}
