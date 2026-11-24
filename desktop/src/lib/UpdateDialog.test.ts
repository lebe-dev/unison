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

import UpdateDialog from "./UpdateDialog.svelte";

/** release returns the offered release of a test. */
function release(overrides: Partial<ReleaseSummary> = {}): ReleaseSummary {
  return {
    version: "0.3.0",
    name: "Unison 0.3.0",
    body: "## What is new\n\n- Faster transfers",
    prerelease: false,
    publishedAt: "2026-11-24T10:00:00Z",
    htmlUrl: "https://github.com/lebe-dev/unison/releases/tag/0.3.0",
    assetSize: 2 * 1024 * 1024,
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

/** open renders the dialog of a state. */
function open(state: UpdaterSnapshot = snapshot()): ReturnType<typeof render> {
  updater.snapshot = state;
  updater.dialogOpen = true;
  return render(UpdateDialog);
}

beforeEach(() => {
  vi.clearAllMocks();
  updater.snapshot = null;
  updater.dialogOpen = false;
  updater.failure = null;
  mocks.onUpdaterState.mockResolvedValue(() => {});
  mocks.getUpdaterState.mockResolvedValue(snapshot());
  mocks.installUpdate.mockResolvedValue(undefined);
  mocks.cancelUpdate.mockResolvedValue(undefined);
});

afterEach(() => {
  updater.snapshot = null;
  updater.dialogOpen = false;
  updater.failure = null;
});

describe("update dialog", () => {
  it("renders the release notes as text", () => {
    const { container } = open();

    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(screen.getByText("What is new")).toBeTruthy();
    expect(screen.getByText("Faster transfers")).toBeTruthy();
    expect(container.querySelector("script")).toBeNull();
  });

  it("puts HTML of the changelog on screen as text", () => {
    const { container } = open(
      snapshot({ available: release({ body: "## Notes\n\n<script>alert(1)</script>" }) }),
    );

    expect(container.querySelector("script")).toBeNull();
    expect(screen.getByText("<script>alert(1)</script>")).toBeTruthy();
  });

  it("shows the version transition and the size", () => {
    open();

    expect(screen.getByText("0.2.5")).toBeTruthy();
    expect(screen.getByText("0.3.0")).toBeTruthy();
    expect(screen.getByText("2.0 MB")).toBeTruthy();
  });

  it("marks a pre-release", () => {
    open(snapshot({ available: release({ prerelease: true }) }));

    expect(screen.getByText("Beta")).toBeTruthy();
  });

  it("shows a fallback for an empty changelog", () => {
    open(snapshot({ available: release({ body: "" }) }));

    expect(screen.getByText("No release notes provided.")).toBeTruthy();
  });

  it("shows the progress of a download and cancels it", async () => {
    open(
      snapshot({
        status: { kind: "downloading", downloaded: 512 * 1024, total: 2 * 1024 * 1024 },
      }),
    );

    expect(screen.getByText(/Downloading 25%/)).toBeTruthy();
    await fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(mocks.cancelUpdate).toHaveBeenCalled();
  });

  it("retries a failed update", async () => {
    open(
      snapshot({
        status: { kind: "error", code: "network", message: "the download stopped" },
      }),
    );

    expect(screen.getByText(/the download stopped/)).toBeTruthy();
    await fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(mocks.installUpdate).toHaveBeenCalled();
  });

  it("explains why a build cannot install the update", () => {
    open(snapshot({ supported: false, unsupportedReason: "the volume is read only" }));

    expect(screen.getByText(/the volume is read only/)).toBeTruthy();
    const install = screen.getByRole("button", { name: "Install update" });
    expect(install.hasAttribute("disabled")).toBe(true);
  });

  it("closes with later", async () => {
    open();

    await fireEvent.click(screen.getByRole("button", { name: "Later" }));

    expect(updater.dialogOpen).toBe(false);
  });

  it("keeps the dialog open during an installation", async () => {
    open(snapshot({ status: { kind: "installing", phase: "applying_update" } }));

    await fireEvent.keyDown(window, { key: "Escape" });

    expect(updater.dialogOpen).toBe(true);
    expect(screen.getByText("Applying the update")).toBeTruthy();
  });

  it("closes with Escape while no installation runs", async () => {
    open();

    await fireEvent.keyDown(window, { key: "Escape" });

    await waitFor(() => expect(updater.dialogOpen).toBe(false));
  });
});
