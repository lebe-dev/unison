import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import {
  ConfirmationScope,
  ConfirmationSide,
  DeviceSchema,
  Role,
  SetDeviceConfirmationResponseSchema,
} from "./api/gen/unison/local/v1/local_pb";
import { client } from "./api/client";
import { confirmation } from "./confirmation.svelte";
import { daemon } from "./daemon.svelte";

vi.mock("./api/client", () => ({
  client: { setDeviceConfirmation: vi.fn() },
}));

vi.mock("./daemon.svelte", () => ({
  daemon: { refresh: vi.fn() },
}));

const setDeviceConfirmation = vi.mocked(client.setDeviceConfirmation);
const refresh = vi.mocked(daemon.refresh);

beforeEach(() => {
  vi.clearAllMocks();
});

describe("confirmation flags of a device", () => {
  it("reports every flag of the device before the user changes it", () => {
    const flags = device("device-a", {
      requireConfirmation: true,
      requireFileConfirmation: false,
      peerRequireConfirmation: false,
      peerRequireFileConfirmation: true,
    });

    expect(confirmation.require(flags, "clipboard", "local")).toBe(true);
    expect(confirmation.require(flags, "files", "local")).toBe(false);
    expect(confirmation.require(flags, "clipboard", "peer")).toBe(false);
    expect(confirmation.require(flags, "files", "peer")).toBe(true);
  });

  it("sends the scope and the side and reloads the status", async () => {
    setDeviceConfirmation.mockResolvedValue(create(SetDeviceConfirmationResponseSchema, {}));
    let shown: boolean | null = null;
    refresh.mockImplementation(() => {
      shown = confirmation.require(device("device-b"), "files", "peer");
      return Promise.resolve();
    });

    expect(await confirmation.set("device-b", "files", "peer", true)).toBeNull();

    expect(setDeviceConfirmation).toHaveBeenCalledWith({
      deviceId: "device-b",
      require: true,
      scope: ConfirmationScope.FILES,
      side: ConfirmationSide.PEER,
    });
    // The new flag shows until the status reports it.
    expect(shown).toBe(true);
    expect(refresh).toHaveBeenCalled();
    // After the reload the status is the source again.
    expect(confirmation.require(device("device-b"), "files", "peer")).toBe(false);
    expect(confirmation.busy).toEqual([]);
  });

  it("keeps the flag of the device when the daemon fails", async () => {
    setDeviceConfirmation.mockRejectedValue(new Error("permission denied"));

    const failure = await confirmation.set("device-d", "clipboard", "local", true);

    expect(failure).toBe("permission denied");
    expect(confirmation.require(device("device-d"), "clipboard", "local")).toBe(false);
    expect(refresh).not.toHaveBeenCalled();
  });

  it("reports a device that is offline", async () => {
    setDeviceConfirmation.mockRejectedValue(new ConnectError("peer offline", Code.Unavailable));

    const failure = await confirmation.set("device-e", "clipboard", "peer", true);

    expect(failure).toBe("The device is offline. Its own setting changes only while it is online.");
  });

  it("marks only the switch in flight as busy", async () => {
    let release = () => {};
    setDeviceConfirmation.mockReturnValue(
      new Promise((resolve) => {
        release = () => resolve(create(SetDeviceConfirmationResponseSchema, {}));
      }),
    );

    const pending = confirmation.set("device-f", "files", "local", true);

    expect(confirmation.isBusy("device-f", "files", "local")).toBe(true);
    expect(confirmation.isBusy("device-f", "clipboard", "local")).toBe(false);
    release();
    await pending;
    expect(confirmation.isBusy("device-f", "files", "local")).toBe(false);
  });
});

function device(
  id: string,
  flags: {
    requireConfirmation?: boolean;
    requireFileConfirmation?: boolean;
    peerRequireConfirmation?: boolean;
    peerRequireFileConfirmation?: boolean;
  } = {},
) {
  return create(DeviceSchema, { deviceId: id, role: Role.PERSONAL, ...flags });
}
