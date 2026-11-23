import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  invoke: vi.fn(),
  listeners: new Map<string, (event: { payload: unknown }) => void>(),
}));

vi.mock("@tauri-apps/api/core", () => ({ invoke: mocks.invoke }));
vi.mock("@tauri-apps/api/event", () => ({
  listen: (name: string, handler: (event: { payload: unknown }) => void) => {
    mocks.listeners.set(name, handler);
    return Promise.resolve(() => mocks.listeners.delete(name));
  },
}));

import { SHARE_FILES_EVENT, onSharedFiles } from "./share";

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listeners.clear();
});

async function settle(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0));
}

describe("shared files", () => {
  it("takes the files that waited before the page loaded", async () => {
    mocks.invoke.mockResolvedValueOnce(["/tmp/a.pdf"]);
    const handler = vi.fn();

    await onSharedFiles(handler);
    await settle();

    expect(mocks.invoke).toHaveBeenCalledWith("take_shared_files");
    expect(handler).toHaveBeenCalledWith(["/tmp/a.pdf"]);
  });

  it("takes the files of every later share", async () => {
    mocks.invoke.mockResolvedValueOnce([]).mockResolvedValueOnce(["/tmp/b.png"]);
    const handler = vi.fn();

    await onSharedFiles(handler);
    await settle();
    mocks.listeners.get(SHARE_FILES_EVENT)?.({ payload: null });
    await settle();

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler).toHaveBeenCalledWith(["/tmp/b.png"]);
  });

  it("stops listening", async () => {
    mocks.invoke.mockResolvedValue([]);

    const unlisten = await onSharedFiles(vi.fn());
    unlisten();

    expect(mocks.listeners.has(SHARE_FILES_EVENT)).toBe(false);
  });

  it("survives a failed read of the inbox", async () => {
    mocks.invoke.mockRejectedValueOnce(new Error("no shell"));
    const handler = vi.fn();
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});

    await onSharedFiles(handler);
    await settle();

    expect(handler).not.toHaveBeenCalled();
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
  });
});
