import { afterEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen } from "@testing-library/svelte";
import { GetStatusResponseSchema } from "./api/gen/unison/local/v1/local_pb";
import DaemonStatus from "./DaemonStatus.svelte";
import { daemon } from "./daemon.svelte";

const REASON = "the login item waits for approval in System Settings";

function running(version: string): void {
  daemon.status = create(GetStatusResponseSchema, { running: true, version });
}

/** offline reports a status call that did not reach the daemon. */
function offline(): void {
  daemon.status = null;
  daemon.error = "cannot reach the daemon: No such file or directory";
}

describe("daemon status", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    daemon.status = null;
    daemon.error = null;
    daemon.installing = false;
    daemon.installError = null;
    daemon.startup = { state: "offline", reason: null, install: "off" };
  });

  it("shows the version of a running daemon", () => {
    running("0.1.0");

    render(DaemonStatus);

    expect(screen.getByText("Daemon 0.1.0")).toBeTruthy();
    expect(screen.queryByText(/Daemon offline/)).toBeNull();
  });

  it("shows the reason of a failed start", () => {
    offline();
    daemon.startup = { state: "failed", reason: REASON, install: "off" };

    render(DaemonStatus);

    expect(screen.getByText("Daemon offline")).toBeTruthy();
    expect(screen.getByText(REASON)).toBeTruthy();
  });

  it("hides the reason once the daemon answers", () => {
    running("0.1.0");
    daemon.startup = { state: "failed", reason: REASON, install: "off" };

    render(DaemonStatus);

    expect(screen.queryByText(REASON)).toBeNull();
  });

  it("shows no reason when no start has failed", () => {
    offline();

    render(DaemonStatus);

    expect(screen.getByText("Daemon offline")).toBeTruthy();
    expect(screen.queryByText(REASON)).toBeNull();
  });

  it("offers the installation of a missing daemon", async () => {
    offline();
    daemon.startup = { state: "failed", reason: REASON, install: "available" };
    const install = vi.spyOn(daemon, "install").mockResolvedValue(undefined);

    render(DaemonStatus);
    await fireEvent.click(screen.getByRole("button", { name: "Install the daemon" }));

    expect(install).toHaveBeenCalledOnce();
  });

  it("shows the progress of the installation", () => {
    offline();
    daemon.startup = { state: "failed", reason: REASON, install: "available" };
    daemon.installing = true;

    render(DaemonStatus);

    const button = screen.getByRole("button", { name: "Installing the daemon..." });
    expect(button).toHaveProperty("disabled", true);
  });

  it("offers no installation while the daemon answers", () => {
    running("0.1.0");
    daemon.startup = { state: "running", reason: null, install: "available" };

    render(DaemonStatus);

    expect(screen.queryByRole("button", { name: "Install the daemon" })).toBeNull();
  });

  it("offers no installation when there is nothing to install", () => {
    offline();
    daemon.startup = { state: "failed", reason: REASON, install: "off" };

    render(DaemonStatus);

    expect(screen.queryByRole("button", { name: "Install the daemon" })).toBeNull();
  });

  it("explains a daemon the app cannot install", () => {
    offline();
    daemon.startup = { state: "failed", reason: REASON, install: "missing" };

    render(DaemonStatus);

    expect(
      screen.getByText("The daemon file is missing next to the app; reinstall Unison."),
    ).toBeTruthy();
    expect(screen.queryByText(REASON)).toBeNull();
    expect(screen.queryByRole("button", { name: "Install the daemon" })).toBeNull();
  });

  it("shows the result of a failed installation", () => {
    offline();
    daemon.startup = { state: "failed", reason: REASON, install: "available" };
    daemon.installError = "the installation was cancelled";

    render(DaemonStatus);

    expect(screen.getByText("the installation was cancelled")).toBeTruthy();
  });
});
