import { invoke } from "@tauri-apps/api/core";
import { client } from "./api/client";
import type { HistoryItem } from "./api/gen/unison/local/v1/local_pb";
import { translate } from "./i18n";
import { isMac } from "./platform";

/** Distance in pixels the pointer travels before a press becomes a drag. */
export const DRAG_THRESHOLD = 4;

/** canDragOut reports whether the system starts a native drag out of the window. */
export function canDragOut(): boolean {
  return isMac();
}

/** crossedDragThreshold reports whether the pointer left the press position far
 * enough to start a drag. */
export function crossedDragThreshold(
  start: { x: number; y: number },
  current: { x: number; y: number },
  threshold = DRAG_THRESHOLD,
): boolean {
  return Math.hypot(current.x - start.x, current.y - start.y) >= threshold;
}

/**
 * stageItem writes the content of an item into a fresh staging directory and
 * returns the absolute paths of the copies. The shell keeps the directory, so a
 * terminal inserts a path that still points at the content.
 */
export async function stageItem(item: HistoryItem): Promise<string[]> {
  const info = item.item;
  if (!info) {
    throw new Error(translate("dashboard.dragFailed"));
  }
  const directory = await invoke<string>("drag_staging_dir", { itemId: info.id });
  const response = await client.fetchHistoryItem({ id: info.id, path: directory });
  if (response.files.length === 0) {
    throw new Error(translate("dashboard.dragFailed"));
  }
  return response.files;
}

/** startStagedDrag hands staged files to the native drag of the system. */
export function startStagedDrag(paths: string[]): Promise<void> {
  return invoke("start_file_drag", { paths });
}

/** dragItem stages an item and starts its native drag at once. */
export async function dragItem(item: HistoryItem): Promise<void> {
  const paths = await stageItem(item);
  await startStagedDrag(paths);
}
