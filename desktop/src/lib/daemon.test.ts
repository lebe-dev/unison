import { beforeEach, describe, expect, it, vi } from "vitest";
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

function offline(): void {
  getStatus.mockRejectedValue(new ConnectError("cannot reach the daemon", Code.Unavailable));
}

describe("daemon store", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    daemon.status = null;
    daemon.error = null;
    daemon.startup = { state: "offline", reason: null, install: "off" };
    daemon.installing = false;
    daemon.installError = null;
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
});
