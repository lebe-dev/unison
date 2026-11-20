import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { GetStatusResponseSchema } from "./api/gen/unison/local/v1/local_pb";
import { client } from "./api/client";
import { daemon } from "./daemon.svelte";
import { daemonStatus, installDaemon } from "./daemonStatus";

vi.mock("./api/client", () => ({
  client: {
    getStatus: vi.fn(),
    setPause: vi.fn(),
  },
}));

vi.mock("./daemonStatus", () => ({
  daemonStatus: vi.fn(),
  installDaemon: vi.fn(),
}));

vi.mock("./tray", () => ({
  updateTray: vi.fn().mockResolvedValue(undefined),
}));

const getStatus = vi.mocked(client.getStatus);
const readStatus = vi.mocked(daemonStatus);
const install = vi.mocked(installDaemon);

const REASON = "the daemon stopped during startup (exit status: 1)";

/** Times of the store that the tests move with fake timers. */
const CONNECT_TIMEOUT = 10_000;
const CONNECT_STEP = 300;

function offline(): void {
  getStatus.mockRejectedValue(new ConnectError("cannot reach the daemon", Code.Unavailable));
}

function unreachable(): ConnectError {
  return new ConnectError("cannot reach the daemon", Code.Unavailable);
}

describe("daemon store", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    daemon.status = null;
    daemon.error = null;
    daemon.connecting = false;
    daemon.startup = { state: "offline", reason: null, install: "off" };
    daemon.installing = false;
    daemon.installError = null;
  });

  afterEach(() => {
    daemon.stop();
    vi.useRealTimers();
  });

  it("reports the local network blocked only while the daemon says so", async () => {
    getStatus.mockResolvedValueOnce(create(GetStatusResponseSchema, { running: true }));
    await daemon.refresh();
    expect(daemon.localNetworkBlocked).toBe(false);

    getStatus.mockResolvedValueOnce(
      create(GetStatusResponseSchema, { running: true, localNetworkBlocked: true }),
    );
    await daemon.refresh();
    expect(daemon.localNetworkBlocked).toBe(true);

    offline();
    readStatus.mockResolvedValue({ state: "offline", reason: null, install: "off" });
    await daemon.refresh();
    expect(daemon.localNetworkBlocked).toBe(false);
  });

  it("keeps the reason of a failed start while the daemon is offline", async () => {
    offline();
    readStatus.mockResolvedValue({ state: "failed", reason: REASON, install: "available" });

    await daemon.refresh();

    expect(daemon.offlineReason).toBe(REASON);
  });

  it("takes the reason of a later attempt", async () => {
    offline();
    readStatus.mockResolvedValueOnce({ state: "offline", reason: null, install: "off" });
    await daemon.refresh();
    expect(daemon.offlineReason).toBeNull();

    readStatus.mockResolvedValueOnce({ state: "failed", reason: REASON, install: "available" });
    await daemon.refresh();

    expect(daemon.offlineReason).toBe(REASON);
  });

  it("drops the reason when the daemon answers", async () => {
    getStatus.mockResolvedValue(create(GetStatusResponseSchema, { running: true }));

    await daemon.refresh();

    expect(daemon.offlineReason).toBeNull();
    expect(readStatus).not.toHaveBeenCalled();
  });

  it("offers the installation of a missing daemon", async () => {
    offline();
    readStatus.mockResolvedValue({ state: "failed", reason: REASON, install: "available" });

    await daemon.refresh();

    expect(daemon.installable).toBe(true);
    expect(daemon.installMissing).toBe(false);
  });

  it("offers no installation of a daemon the app has no copy of", async () => {
    offline();
    readStatus.mockResolvedValue({ state: "failed", reason: REASON, install: "missing" });

    await daemon.refresh();

    expect(daemon.installable).toBe(false);
    expect(daemon.installMissing).toBe(true);
  });

  it("installs the daemon and connects to it", async () => {
    offline();
    readStatus.mockResolvedValue({ state: "failed", reason: REASON, install: "available" });
    await daemon.refresh();
    install.mockResolvedValue(undefined);
    getStatus.mockResolvedValueOnce(create(GetStatusResponseSchema, { running: true }));

    await daemon.install();

    expect(install).toHaveBeenCalledOnce();
    expect(daemon.connected).toBe(true);
    expect(daemon.installable).toBe(false);
    expect(daemon.installError).toBeNull();
  });

  it("keeps the reason of an installation that did not happen", async () => {
    offline();
    readStatus.mockResolvedValue({ state: "failed", reason: REASON, install: "available" });
    await daemon.refresh();
    install.mockRejectedValue("the installation was cancelled");

    await daemon.install();

    expect(daemon.installable).toBe(true);
    expect(daemon.installError).toBe("the installation was cancelled");
    expect(daemon.installing).toBe(false);
  });

  it("waits for the daemon that is still starting", async () => {
    vi.useFakeTimers();
    offline();
    readStatus.mockResolvedValue({ state: "offline", reason: null, install: "off" });
    getStatus
      .mockRejectedValueOnce(unreachable())
      .mockRejectedValueOnce(unreachable())
      .mockResolvedValue(create(GetStatusResponseSchema, { running: true }));

    const started = daemon.start();
    await vi.advanceTimersByTimeAsync(CONNECT_STEP);

    expect(daemon.loading).toBe(true);
    expect(daemon.connected).toBe(false);

    await vi.advanceTimersByTimeAsync(CONNECT_STEP);
    await started;

    expect(daemon.connected).toBe(true);
    expect(daemon.loading).toBe(false);
  });

  it("reports a daemon that stays away after the timeout", async () => {
    vi.useFakeTimers();
    offline();
    readStatus.mockResolvedValue({ state: "offline", reason: null, install: "off" });

    const started = daemon.start();
    await vi.advanceTimersByTimeAsync(CONNECT_TIMEOUT);

    expect(daemon.loading).toBe(true);

    await vi.advanceTimersByTimeAsync(CONNECT_STEP);
    await started;

    expect(daemon.loading).toBe(false);
    expect(daemon.connected).toBe(false);
    expect(daemon.error).not.toBeNull();
  });

  it("reports a failed start without waiting for the timeout", async () => {
    vi.useFakeTimers();
    offline();
    readStatus.mockResolvedValue({ state: "failed", reason: REASON, install: "available" });

    const started = daemon.start();
    await vi.advanceTimersByTimeAsync(0);
    await started;

    expect(daemon.loading).toBe(false);
    expect(daemon.failed).toBe(true);
    expect(daemon.offlineReason).toBe(REASON);
  });

  it("waits for the daemon again when the user retries", async () => {
    vi.useFakeTimers();
    offline();
    readStatus.mockResolvedValue({ state: "offline", reason: null, install: "off" });
    await daemon.refresh();
    expect(daemon.loading).toBe(false);

    getStatus.mockRejectedValueOnce(unreachable());
    getStatus.mockResolvedValue(create(GetStatusResponseSchema, { running: true }));

    const retried = daemon.retry();
    await vi.advanceTimersByTimeAsync(CONNECT_STEP);
    await retried;

    expect(daemon.connected).toBe(true);
    expect(daemon.loading).toBe(false);
  });
});
