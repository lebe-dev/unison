import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { updater } from "./updater.svelte";
import type { ReleaseSummary, UpdaterSnapshot } from "./updater";

const mocks = vi.hoisted(() => ({
  dismissUpdate: vi.fn(),
  getUpdaterState: vi.fn(),
  onUpdaterState: vi.fn(),
  setConfig: vi.fn(),
  checkNow: vi.fn(),
  installUpdate: vi.fn(),
  cancelUpdate: vi.fn(),
}));

vi.mock("./updater", () => mocks);

import UpdateBanner from "./UpdateBanner.svelte";

/** release returns the offered release of a test. */
function release(overrides: Partial<ReleaseSummary> = {}): ReleaseSummary {
  return {
    version: "0.3.0",
    name: "Unison 0.3.0",
    body: "## What is new",
    prerelease: false,
    publishedAt: "2026-11-24T10:00:00Z",
    htmlUrl: "https://github.com/lebe-dev/unison/releases/tag/0.3.0",
    assetSize: 1024,
    ...overrides,
  };
}

/** snapshot returns the state of the updater of a test. */
function snapshot(overrides: Partial<UpdaterSnapshot> = {}): UpdaterSnapshot {
  return {
    currentVersion: "0.2.5",
    supported: true,
    unsupportedReason: null,
    config: {
      enabled: false,
      betaChannel: false,
      interval: "daily",
      lastCheckedUnixMs: null,
      dismissedVersion: null,
      lastEtag: null,
    },
    status: { kind: "available" },
    available: release(),
    dismissed: false,
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  updater.snapshot = null;
  updater.dialogOpen = false;
  updater.failure = null;
  mocks.onUpdaterState.mockResolvedValue(() => {});
  mocks.getUpdaterState.mockResolvedValue(snapshot());
});

afterEach(() => {
  updater.snapshot = null;
  updater.dialogOpen = false;
  updater.failure = null;
});

describe("update banner", () => {
  it("shows nothing without an update", () => {
    const { container } = render(UpdateBanner);

    expect(container.querySelector(".update-banner")).toBeNull();
  });

  it("names the available version", () => {
    updater.snapshot = snapshot();

    render(UpdateBanner);

    expect(screen.getByText("Unison 0.3.0 is available")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Update" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Later" })).toBeTruthy();
  });

  it("marks a pre-release", () => {
    updater.snapshot = snapshot({ available: release({ prerelease: true }) });

    render(UpdateBanner);

    expect(screen.getByText("Beta")).toBeTruthy();
  });

  it("opens the dialog from the update button", async () => {
    updater.snapshot = snapshot();
    render(UpdateBanner);

    await fireEvent.click(screen.getByRole("button", { name: "Update" }));

    expect(updater.dialogOpen).toBe(true);
  });

  it("puts the release aside with later", async () => {
    updater.snapshot = snapshot();
    mocks.dismissUpdate.mockResolvedValue(snapshot({ dismissed: true }));
    render(UpdateBanner);

    await fireEvent.click(screen.getByRole("button", { name: "Later" }));

    await waitFor(() => expect(mocks.dismissUpdate).toHaveBeenCalledWith("0.3.0"));
    expect(updater.showBanner).toBe(false);
  });

  it("hides the banner when the user put the release aside", () => {
    updater.snapshot = snapshot({ dismissed: true });

    const { container } = render(UpdateBanner);

    expect(container.querySelector(".update-banner")).toBeNull();
  });

  it("shows the progress of a running download", () => {
    updater.snapshot = snapshot({
      status: { kind: "downloading", downloaded: 512, total: 1024 },
    });

    render(UpdateBanner);

    expect(screen.getByText(/Downloading 50%/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Show details" })).toBeTruthy();
  });
});
