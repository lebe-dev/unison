import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { GetStatusResponseSchema } from "./api/gen/unison/local/v1/local_pb";
import { client } from "./api/client";
import { daemon } from "./daemon.svelte";
import { daemonStatus } from "./daemonStatus";

vi.mock("./api/client", () => ({
  client: {
    getStatus: vi.fn(),
    setPause: vi.fn(),
  },
}));

vi.mock("./daemonStatus", () => ({
  daemonStatus: vi.fn(),
}));

vi.mock("./tray", () => ({
  updateTray: vi.fn().mockResolvedValue(undefined),
}));

const getStatus = vi.mocked(client.getStatus);
const readStatus = vi.mocked(daemonStatus);

const REASON = "the daemon stopped during startup (exit status: 1)";

function offline(): void {
  getStatus.mockRejectedValue(new ConnectError("cannot reach the daemon", Code.Unavailable));
}

describe("daemon store", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    daemon.status = null;
    daemon.error = null;
    daemon.startup = { state: "offline", reason: null };
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
    readStatus.mockResolvedValue({ state: "offline", reason: null });
    await daemon.refresh();
    expect(daemon.localNetworkBlocked).toBe(false);
  });

  it("keeps the reason of a failed start while the daemon is offline", async () => {
    offline();
    readStatus.mockResolvedValue({ state: "failed", reason: REASON });

    await daemon.refresh();

    expect(daemon.offlineReason).toBe(REASON);
  });

  it("takes the reason of a later attempt", async () => {
    offline();
    readStatus.mockResolvedValueOnce({ state: "offline", reason: null });
    await daemon.refresh();
    expect(daemon.offlineReason).toBeNull();

    readStatus.mockResolvedValueOnce({ state: "failed", reason: REASON });
    await daemon.refresh();

    expect(daemon.offlineReason).toBe(REASON);
  });

  it("drops the reason when the daemon answers", async () => {
    getStatus.mockResolvedValue(create(GetStatusResponseSchema, { running: true }));

    await daemon.refresh();

    expect(daemon.offlineReason).toBeNull();
    expect(readStatus).not.toHaveBeenCalled();
  });
});
