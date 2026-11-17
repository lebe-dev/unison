import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { GetHistoryThumbnailResponseSchema } from "./api/gen/unison/local/v1/local_pb";
import { client } from "./api/client";
import { clearThumbnails, loadThumbnail, pixelsOf } from "./thumbnails";

vi.mock("./api/client", () => ({
  client: {
    getHistoryThumbnail: vi.fn(),
  },
}));

const getHistoryThumbnail = vi.mocked(client.getHistoryThumbnail);

beforeEach(() => {
  vi.clearAllMocks();
  clearThumbnails();
});

describe("pixelsOf", () => {
  it("accepts rows of four bytes per pixel", () => {
    const response = create(GetHistoryThumbnailResponseSchema, {
      width: 2,
      height: 1,
      rgba: new Uint8Array([1, 2, 3, 4, 5, 6, 7, 8]),
    });
    expect(pixelsOf(response)).toEqual({
      width: 2,
      height: 1,
      rgba: new Uint8ClampedArray([1, 2, 3, 4, 5, 6, 7, 8]),
    });
  });

  it("refuses a size that does not match the pixels", () => {
    const short = create(GetHistoryThumbnailResponseSchema, {
      width: 2,
      height: 2,
      rgba: new Uint8Array(8),
    });
    const empty = create(GetHistoryThumbnailResponseSchema, { width: 0, height: 0 });
    expect(pixelsOf(short)).toBeNull();
    expect(pixelsOf(empty)).toBeNull();
  });
});

describe("loadThumbnail", () => {
  it("asks the daemon once per item and size", async () => {
    getHistoryThumbnail.mockResolvedValue(
      create(GetHistoryThumbnailResponseSchema, {
        width: 1,
        height: 1,
        rgba: new Uint8Array([9, 9, 9, 255]),
      }),
    );

    const first = await loadThumbnail("item-1", 128);
    const second = await loadThumbnail("item-1", 128);
    await loadThumbnail("item-1", 1024);

    expect(first).toEqual(second);
    expect(getHistoryThumbnail).toHaveBeenCalledTimes(2);
    expect(getHistoryThumbnail).toHaveBeenCalledWith({ id: "item-1", maxSide: 128 });
    expect(getHistoryThumbnail).toHaveBeenCalledWith({ id: "item-1", maxSide: 1024 });
  });

  it("returns null and retries later when the daemon fails", async () => {
    getHistoryThumbnail.mockRejectedValueOnce(new Error("payload missing"));
    expect(await loadThumbnail("item-2", 128)).toBeNull();

    getHistoryThumbnail.mockResolvedValueOnce(
      create(GetHistoryThumbnailResponseSchema, {
        width: 1,
        height: 1,
        rgba: new Uint8Array([1, 1, 1, 255]),
      }),
    );
    expect(await loadThumbnail("item-2", 128)).not.toBeNull();
  });
});
