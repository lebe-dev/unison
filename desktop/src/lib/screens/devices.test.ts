import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, render, screen } from "@testing-library/svelte";
import {
  DeviceSchema,
  GetStatusResponseSchema,
  Platform,
  RevokeDeviceResponseSchema,
  Role,
  SetDeviceConfirmationResponseSchema,
} from "../api/gen/unison/local/v1/local_pb";
import { client } from "../api/client";
import { daemon } from "../daemon.svelte";
import Devices from "./Devices.svelte";

vi.mock("../api/client", () => ({
  client: {
    getStatus: vi.fn(),
    setDeviceConfirmation: vi.fn(),
    revokeDevice: vi.fn(),
  },
}));

const getStatus = vi.mocked(client.getStatus);
const setDeviceConfirmation = vi.mocked(client.setDeviceConfirmation);
const revokeDevice = vi.mocked(client.revokeDevice);

beforeEach(() => {
  vi.clearAllMocks();
  getStatus.mockResolvedValue(
    create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [personal("device-1"), friends("device-2")],
    }),
  );
  setDeviceConfirmation.mockResolvedValue(create(SetDeviceConfirmationResponseSchema, {}));
  revokeDevice.mockResolvedValue(create(RevokeDeviceResponseSchema, {}));
  daemon.status = create(GetStatusResponseSchema, {
    running: true,
    deviceId: "self",
    deviceName: "This Mac",
    devices: [personal("device-1"), friends("device-2")],
  });
  daemon.error = null;
});

describe("devices screen", () => {
  it("offers the confirmation flag to Personal devices only", () => {
    render(Devices);

    expect(screen.getByRole("checkbox", { name: /Hold incoming items/ })).toBeTruthy();
    expect(screen.getAllByRole("checkbox")).toHaveLength(1);
  });

  it("asks a Personal device to hold incoming items", async () => {
    render(Devices);
    const toggle = screen.getByRole("checkbox", { name: /Hold incoming items/ });

    await fireEvent.click(toggle);

    expect(setDeviceConfirmation).toHaveBeenCalledWith({
      deviceId: "device-1",
      require: true,
    });
    expect((toggle as HTMLInputElement).checked).toBe(true);
  });

  it("reports a device that cannot be reached", async () => {
    setDeviceConfirmation.mockRejectedValue(new ConnectError("peer offline", Code.Unavailable));

    render(Devices);
    await fireEvent.click(screen.getByRole("checkbox", { name: /Hold incoming items/ }));

    expect(await screen.findByText(/The device is offline/)).toBeTruthy();
  });

  it("revokes a device only after a confirmation", async () => {
    render(Devices);

    await fireEvent.click(screen.getAllByRole("button", { name: "Revoke" })[0]);
    expect(screen.getByText(/stops\s+trusting it/)).toBeTruthy();
    await fireEvent.click(screen.getByRole("button", { name: "Revoke device" }));

    expect(revokeDevice).toHaveBeenCalledWith({ deviceId: "device-1" });
    expect(getStatus).toHaveBeenCalled();
  });

  it("keeps the device when the confirmation is canceled", async () => {
    render(Devices);

    await fireEvent.click(screen.getAllByRole("button", { name: "Revoke" })[0]);
    await fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(revokeDevice).not.toHaveBeenCalled();
  });
});

function personal(id: string) {
  return create(DeviceSchema, {
    deviceId: id,
    name: "MacBook",
    role: Role.PERSONAL,
    platform: Platform.MACOS,
    online: true,
  });
}

function friends(id: string) {
  return create(DeviceSchema, {
    deviceId: id,
    name: "Phone",
    role: Role.FRIENDS,
    platform: Platform.ANDROID,
  });
}
