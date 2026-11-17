import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, render, screen } from "@testing-library/svelte";
import {
  ConfirmationScope,
  ConfirmationSide,
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
  it("offers the four confirmation flags to Personal devices only", () => {
    render(Devices);

    expect(screen.getAllByRole("checkbox")).toHaveLength(4);
    expect(
      screen.getByRole("checkbox", {
        name: "This device asks before applying the clipboard of MacBook",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("checkbox", {
        name: "This device asks before accepting files from MacBook",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("checkbox", {
        name: "MacBook asks before applying the clipboard of this device",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("checkbox", {
        name: "MacBook asks before accepting files from this device",
      }),
    ).toBeTruthy();
  });

  it("changes the files flag of this device", async () => {
    render(Devices);
    const toggle = screen.getByRole("checkbox", {
      name: "This device asks before accepting files from MacBook",
    });

    await fireEvent.click(toggle);

    expect(setDeviceConfirmation).toHaveBeenCalledWith({
      deviceId: "device-1",
      require: true,
      scope: ConfirmationScope.FILES,
      side: ConfirmationSide.LOCAL,
    });
  });

  it("asks the paired device to change its clipboard flag", async () => {
    render(Devices);

    await fireEvent.click(
      screen.getByRole("checkbox", {
        name: "MacBook asks before applying the clipboard of this device",
      }),
    );

    expect(setDeviceConfirmation).toHaveBeenCalledWith({
      deviceId: "device-1",
      require: true,
      scope: ConfirmationScope.CLIPBOARD,
      side: ConfirmationSide.PEER,
    });
  });

  it("reports a device that cannot be reached", async () => {
    setDeviceConfirmation.mockRejectedValue(new ConnectError("peer offline", Code.Unavailable));

    render(Devices);
    await fireEvent.click(
      screen.getByRole("checkbox", {
        name: "MacBook asks before accepting files from this device",
      }),
    );

    expect(await screen.findByText(/The device is offline/)).toBeTruthy();
  });

  it("shows the flags the device reported", () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [personal("device-1", { peerRequireFileConfirmation: true })],
    });

    render(Devices);

    const files = screen.getByRole("checkbox", {
      name: "MacBook asks before accepting files from this device",
    });
    const clipboard = screen.getByRole("checkbox", {
      name: "MacBook asks before applying the clipboard of this device",
    });
    expect((files as HTMLInputElement).checked).toBe(true);
    expect((clipboard as HTMLInputElement).checked).toBe(false);
  });

  it("keeps the flags of an offline device read only", () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [personal("device-1", { online: false })],
    });

    render(Devices);

    const peer = screen.getByRole("checkbox", {
      name: "MacBook asks before accepting files from this device",
    });
    const local = screen.getByRole("checkbox", {
      name: "This device asks before accepting files from MacBook",
    });
    expect((peer as HTMLInputElement).disabled).toBe(true);
    expect((local as HTMLInputElement).disabled).toBe(false);
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

function personal(
  id: string,
  fields: {
    peerRequireConfirmation?: boolean;
    peerRequireFileConfirmation?: boolean;
    online?: boolean;
  } = {},
) {
  return create(DeviceSchema, {
    deviceId: id,
    name: "MacBook",
    role: Role.PERSONAL,
    platform: Platform.MACOS,
    online: true,
    ...fields,
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
