import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/svelte";
import { tick } from "svelte";

const mocks = vi.hoisted(() => ({
  emit: vi.fn(),
  listeners: new Map<string, (event: { payload: unknown }) => void>(),
}));

vi.mock("@tauri-apps/api/event", () => ({
  emit: mocks.emit,
  listen: (name: string, handler: (event: { payload: unknown }) => void) => {
    mocks.listeners.set(name, handler);
    return Promise.resolve(() => mocks.listeners.delete(name));
  },
}));

import PromptWindow from "./PromptWindow.svelte";
import type { Prompt } from "./types";

const files: Prompt = {
  id: "offer-1",
  kind: "files",
  device: "Pixel",
  summary: "report.pdf",
  size: "1.0 MB",
};
const clip: Prompt = {
  id: "item-1",
  kind: "clipboard",
  device: "MacBook",
  summary: "Text",
  size: "12 B",
};

async function show(queue: Prompt[]): Promise<void> {
  await vi.waitFor(() => expect(mocks.listeners.has("prompts")).toBe(true));
  mocks.listeners.get("prompts")?.({ payload: queue });
  await tick();
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listeners.clear();
  mocks.emit.mockResolvedValue(undefined);
});

describe("prompt window", () => {
  it("asks for the questions once it listens", async () => {
    render(PromptWindow);

    await vi.waitFor(() => expect(mocks.emit).toHaveBeenCalledWith("prompt-ready"));
  });

  it("asks about the oldest question and answers it", async () => {
    render(PromptWindow);
    await show([clip, files]);

    expect(screen.getByText("Clipboard from MacBook")).toBeTruthy();
    expect(screen.getByText("1 of 2")).toBeTruthy();

    await fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    expect(mocks.emit).toHaveBeenCalledWith("prompt-answer", { id: "item-1", action: "apply" });
    // The next question shows before the main window confirms the answer.
    expect(screen.getByText("Files from Pixel")).toBeTruthy();
    expect(screen.getByText("report.pdf")).toBeTruthy();
  });

  it("declines files and puts the window away on later", async () => {
    render(PromptWindow);
    await show([files]);

    await fireEvent.click(screen.getByRole("button", { name: "Later" }));
    expect(mocks.emit).toHaveBeenCalledWith("prompt-answer", { id: "offer-1", action: "later" });
    expect(screen.getByText("Files from Pixel")).toBeTruthy();

    await fireEvent.click(screen.getByRole("button", { name: "Decline" }));
    expect(mocks.emit).toHaveBeenCalledWith("prompt-answer", { id: "offer-1", action: "decline" });
  });
});
