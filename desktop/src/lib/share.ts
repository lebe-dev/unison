import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";

/** Event the shell sends when another app shared files with Unison. */
export const SHARE_FILES_EVENT = "share-files";

/** takeSharedFiles returns the shared files that wait for an offer. */
function takeSharedFiles(): Promise<string[]> {
  return invoke<string[]>("take_shared_files");
}

/**
 * onSharedFiles runs handler with the files of every share. The shell keeps the
 * files until the page takes them, so the files of the share that started the
 * app arrive as well. The returned function stops the subscription.
 */
export async function onSharedFiles(handler: (paths: string[]) => void): Promise<UnlistenFn> {
  const drain = (): void => {
    takeSharedFiles()
      .then((paths) => {
        if (paths.length > 0) {
          handler(paths);
        }
      })
      .catch((error: unknown) => {
        console.warn("cannot read the shared files", error);
      });
  };
  const unlisten = await listen(SHARE_FILES_EVENT, drain);
  drain();
  return unlisten;
}
