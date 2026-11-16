import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { DeviceSchema, Role, SetDeviceConfirmationResponseSchema } from "./api/gen/unison/local/v1/local_pb";
import { client } from "./api/client";
import { confirmation } from "./confirmation.svelte";

vi.mock("./api/client", () => ({
  client: { setDeviceConfirmation: vi.fn() },
}));

const setDeviceConfirmation = vi.mocked(client.setDeviceConfirmation);

beforeEach(() => {
  vi.clearAllMocks();
});

describe("confirmation flag of a device", () => {
  it("reports the flag of the device before the user changes it", () => {
    expect(confirmation.require(device("device-a", false))).toBe(false);
    expect(confirmation.require(device("device-b", true))).toBe(true);
  });

  it("keeps the flag the user set", async () => {
    setDeviceConfirmation.mockResolvedValue(create(SetDeviceConfirmationResponseSchema, {}));

    expect(await confirmation.set("device-c", true)).toBeNull();

    expect(setDeviceConfirmation).toHaveBeenCalledWith({ deviceId: "device-c", require: true });
    expect(confirmation.require(device("device-c", false))).toBe(true);
    expect(confirmation.busy).toEqual([]);
  });

  it("keeps the flag of the device when the daemon fails", async () => {
    setDeviceConfirmation.mockRejectedValue(new Error("permission denied"));

    const failure = await confirmation.set("device-d", true);

    expect(failure).toBe("permission denied");
    expect(confirmation.require(device("device-d", false))).toBe(false);
  });

  it("reports a device that is offline", async () => {
    setDeviceConfirmation.mockRejectedValue(new ConnectError("peer offline", Code.Unavailable));

    const failure = await confirmation.set("device-e", true);

    expect(failure).toBe("The device is offline, so its flag cannot be changed.");
  });
});

function device(id: string, requireConfirmation: boolean) {
  return create(DeviceSchema, { deviceId: id, role: Role.PERSONAL, requireConfirmation });
}
