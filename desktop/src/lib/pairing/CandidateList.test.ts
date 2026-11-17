import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/svelte";
import { tick } from "svelte";
import CandidateList from "./CandidateList.svelte";
import { quietWindowDelay } from "../network";

describe("CandidateList", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("warns about a quiet network once the window stayed empty for a while", async () => {
    vi.useFakeTimers();
    render(CandidateList, {
      props: { candidates: [], openedAt: Date.now(), listenAddr: "0.0.0.0:5000", onSelect: () => {} },
    });
    expect(screen.getByText(/No devices yet/)).toBeTruthy();

    vi.advanceTimersByTime(quietWindowDelay + 1_000);
    await tick();

    expect(screen.getByText("No devices found on this network yet.")).toBeTruthy();
    expect(screen.getByText(/This window listens at 0.0.0.0:5000/)).toBeTruthy();
  });
});
