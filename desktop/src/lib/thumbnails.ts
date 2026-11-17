import { client } from "./api/client";
import type { GetHistoryThumbnailResponse } from "./api/gen/unison/local/v1/local_pb";

/** Decoded pixels of an image, ready for a canvas. */
export interface Pixels {
  width: number;
  height: number;
  rgba: Uint8ClampedArray<ArrayBuffer>;
}

/** Largest number of thumbnails kept in memory. */
const CACHE_LIMIT = 64;

/** Thumbnails by item and size, oldest first. */
const cache = new Map<string, Promise<Pixels | null>>();

/** pixelsOf checks the answer of the daemon and returns its pixels, or null
 * when the size does not match the bytes. */
export function pixelsOf(response: GetHistoryThumbnailResponse): Pixels | null {
  const { width, height, rgba } = response;
  if (width === 0 || height === 0 || rgba.length !== width * height * 4) {
    return null;
  }
  const copy = new Uint8ClampedArray(new ArrayBuffer(rgba.length));
  copy.set(rgba);
  return { width, height, rgba: copy };
}

/** loadThumbnail returns the pixels of an image item, decoded by the daemon,
 * or null when it has none. The page never decodes an image itself. */
export function loadThumbnail(id: string, maxSide: number): Promise<Pixels | null> {
  const key = `${id}:${maxSide}`;
  const cached = cache.get(key);
  if (cached) {
    return cached;
  }

  const pending = client
    .getHistoryThumbnail({ id, maxSide })
    .then(pixelsOf)
    .catch(() => null)
    .then((pixels) => {
      if (!pixels) {
        cache.delete(key);
      }
      return pixels;
    });
  cache.set(key, pending);
  while (cache.size > CACHE_LIMIT) {
    const oldest = cache.keys().next().value;
    if (oldest === undefined) {
      break;
    }
    cache.delete(oldest);
  }
  return pending;
}

/** clearThumbnails forgets every loaded thumbnail. */
export function clearThumbnails(): void {
  cache.clear();
}
