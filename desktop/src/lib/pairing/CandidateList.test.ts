import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/svelte";
import { create } from "@bufbuild/protobuf";
import { tick } from "svelte";
import { PairingCandidateSchema } from "../api/gen/unison/local/v1/local_pb";
import CandidateList from "./CandidateList.svelte";
import { quietWindowDelay } from "../network";

function candidate(addr: string, txt: Record<string, string> = {}) {
  return create(PairingCandidateSchema, { addr, txt });
}

describe("CandidateList", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("warns about a quiet network once the window stayed empty for a while", async () => {
    vi.useFakeTimers();
    render(CandidateList, {
      props: {
        candidates: [],
        openedAt: Date.now(),
        listenAddr: "0.0.0.0:5000",
        onSelect: () => {},
      },
    });
    expect(screen.getByText(/No devices yet/)).toBeTruthy();

    vi.advanceTimersByTime(quietWindowDelay + 1_000);
    await tick();

    expect(screen.getByText("No devices found on this network yet.")).toBeTruthy();
    expect(screen.getByText(/This window listens at 0.0.0.0:5000/)).toBeTruthy();
  });

  it("shows a relay badge, selects the relay address, and hides the IPv4/IPv6 hint", async () => {
    const onSelect = vi.fn();
    render(CandidateList, {
      props: {
        candidates: [candidate("relay:0123456789abcdef", { transport: "relay" })],
        onSelect,
      },
    });

    expect(screen.getByText("relay:0123456789abcdef")).toBeTruthy();
    expect(screen.getByText("Via relay")).toBeTruthy();
    expect(screen.queryByText(/IPv4 and IPv6/)).toBeNull();

    await fireEvent.click(screen.getByRole("button", { name: "Compare code" }));
    expect(onSelect).toHaveBeenCalledWith("relay:0123456789abcdef");
  });

  it("detects relay candidates by address prefix even without txt metadata", () => {
    render(CandidateList, {
      props: {
        candidates: [candidate("relay:fedcba9876543210")],
        onSelect: () => {},
      },
    });

    expect(screen.getByText("Via relay")).toBeTruthy();
    expect(screen.queryByText(/IPv4 and IPv6/)).toBeNull();
  });

  it("renders mixed local and relay candidates with the hint", async () => {
    const onSelect = vi.fn();
    render(CandidateList, {
      props: {
        candidates: [
          candidate("192.168.1.10:51234"),
          candidate("relay:0123456789abcdef", { transport: "relay" }),
        ],
        onSelect,
      },
    });

    expect(screen.getByText("192.168.1.10:51234")).toBeTruthy();
    expect(screen.getByText("relay:0123456789abcdef")).toBeTruthy();
    expect(screen.getAllByText("Via relay")).toHaveLength(1);
    expect(screen.getByText(/IPv4 and IPv6/)).toBeTruthy();

    const buttons = screen.getAllByRole("button", { name: "Compare code" });
    expect(buttons).toHaveLength(2);
    await fireEvent.click(buttons[0]);
    await fireEvent.click(buttons[1]);
    expect(onSelect).toHaveBeenNthCalledWith(1, "192.168.1.10:51234");
    expect(onSelect).toHaveBeenNthCalledWith(2, "relay:0123456789abcdef");
  });
});
