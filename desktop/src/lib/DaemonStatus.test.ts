import { afterEach, describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { render, screen } from "@testing-library/svelte";
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
    daemon.status = null;
    daemon.error = null;
    daemon.startup = { state: "offline", reason: null };
  });

  it("shows the version of a running daemon", () => {
    running("0.1.0");

    render(DaemonStatus);

    expect(screen.getByText("Daemon 0.1.0")).toBeTruthy();
    expect(screen.queryByText(/Daemon offline/)).toBeNull();
  });

  it("shows the reason of a failed start", () => {
    offline();
    daemon.startup = { state: "failed", reason: REASON };

    render(DaemonStatus);

    expect(screen.getByText("Daemon offline")).toBeTruthy();
    expect(screen.getByText(REASON)).toBeTruthy();
  });

  it("hides the reason once the daemon answers", () => {
    running("0.1.0");
    daemon.startup = { state: "failed", reason: REASON };

    render(DaemonStatus);

    expect(screen.queryByText(REASON)).toBeNull();
  });

  it("shows no reason when no start has failed", () => {
    offline();

    render(DaemonStatus);

    expect(screen.getByText("Daemon offline")).toBeTruthy();
    expect(screen.queryByText(REASON)).toBeNull();
  });
});
