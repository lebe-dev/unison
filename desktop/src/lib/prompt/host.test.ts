import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  invoke: vi.fn(),
  emit: vi.fn(),
  listeners: new Map<string, (event: { payload: unknown }) => void>(),
}));

vi.mock("@tauri-apps/api/core", () => ({ invoke: mocks.invoke }));
vi.mock("@tauri-apps/api/event", () => ({
  emit: mocks.emit,
  listen: async (name: string, handler: (event: { payload: unknown }) => void) => {
    mocks.listeners.set(name, handler);
    return () => mocks.listeners.delete(name);
  },
}));

import { PromptHost } from "./host";
import type { Prompt } from "./types";

function prompt(id: string): Prompt {
  return { id, kind: "files", device: "Pixel", summary: "a.txt", size: "1 B" };
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.invoke.mockResolvedValue(undefined);
  mocks.emit.mockResolvedValue(undefined);
  mocks.listeners.clear();
});

describe("prompt host", () => {
  it("brings the window up only for a new question", () => {
    const host = new PromptHost(vi.fn());

    host.sync([prompt("a")]);
    host.sync([prompt("a")]);
    host.sync([prompt("a"), prompt("b")]);

    const shows = mocks.invoke.mock.calls.filter(([command]) => command === "show_prompt");
    expect(shows).toHaveLength(2);
    expect(mocks.emit).toHaveBeenLastCalledWith("prompts", [prompt("a"), prompt("b")]);
  });

  it("hides the window when nothing waits", () => {
    const host = new PromptHost(vi.fn());

    host.sync([prompt("a")]);
    host.sync([]);

    expect(mocks.invoke).toHaveBeenLastCalledWith("hide_prompt");
  });

  it("answers the window and hides it on later", async () => {
    const onAnswer = vi.fn();
    const host = new PromptHost(onAnswer);
    await host.start();
    host.sync([prompt("a")]);
    mocks.emit.mockClear();

    mocks.listeners.get("prompt-ready")?.({ payload: null });
    mocks.listeners.get("prompt-answer")?.({ payload: { id: "a", action: "accept" } });
    mocks.listeners.get("prompt-answer")?.({ payload: { id: "a", action: "later" } });

    expect(mocks.emit).toHaveBeenCalledWith("prompts", [prompt("a")]);
    expect(onAnswer).toHaveBeenCalledTimes(1);
    expect(onAnswer).toHaveBeenCalledWith({ id: "a", action: "accept" });
    expect(mocks.invoke).toHaveBeenLastCalledWith("hide_prompt");
  });
});
