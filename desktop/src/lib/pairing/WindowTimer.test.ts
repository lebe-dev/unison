import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/svelte";
import { tick } from "svelte";
import WindowTimer from "./WindowTimer.svelte";

describe("WindowTimer", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("counts down to the deadline and reports it once", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T12:00:00Z"));
    const onExpire = vi.fn();
    const expiresAt = Date.now() + 125_000;

    const { container, unmount } = render(WindowTimer, { props: { expiresAt, onExpire } });
    expect(screen.getByText("2:05")).toBeTruthy();

    await vi.advanceTimersByTimeAsync(60_000);
    await tick();
    expect(screen.getByText("1:05")).toBeTruthy();
    expect(onExpire).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(65_000);
    await tick();
    expect(onExpire).toHaveBeenCalledTimes(1);
    expect(container.textContent).toContain("Closing the window");

    await vi.advanceTimersByTimeAsync(5_000);
    await tick();
    expect(onExpire).toHaveBeenCalledTimes(1);

    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("marks the last thirty seconds", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T12:00:00Z"));
    const { container } = render(WindowTimer, { props: { expiresAt: Date.now() + 20_000 } });

    await tick();
    expect(container.querySelector(".timer")?.classList.contains("urgent")).toBe(true);
  });

  it("restarts the countdown when the window is extended", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T12:00:00Z"));
    const start = Date.now();
    const { rerender } = render(WindowTimer, { props: { expiresAt: start + 125_000 } });

    await vi.advanceTimersByTimeAsync(60_000);
    await tick();
    expect(screen.getByText("1:05")).toBeTruthy();

    await rerender({ expiresAt: Date.now() + 180_000 });
    await tick();
    expect(screen.getByText("3:00")).toBeTruthy();
  });
});
