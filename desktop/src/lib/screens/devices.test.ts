import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, render, screen } from "@testing-library/svelte";
import { tick } from "svelte";
import {
  CancelMediatedPairResponseSchema,
  ConfirmationScope,
  ConfirmationSide,
  DeviceSchema,
  GetStatusResponseSchema,
  MediatedDeviceSchema,
  MediatedPairDirection,
  MediatedPairSchema,
  MediatedPairState,
  Platform,
  RetryDeviceResponseSchema,
  RevokeDeviceResponseSchema,
  Role,
  SasSchema,
  SetDeviceConfirmationResponseSchema,
  SetDeviceRemoteSettingsResponseSchema,
  StartMediatedPairResponseSchema,
} from "../api/gen/unison/local/v1/local_pb";
import { client } from "../api/client";
import { daemon } from "../daemon.svelte";
import { mediated } from "../mediated.svelte";
import Devices from "./Devices.svelte";

vi.mock("../api/client", () => ({
  client: {
    getStatus: vi.fn(),
    retryDevice: vi.fn(),
    setDeviceConfirmation: vi.fn(),
    setDeviceRemoteSettings: vi.fn(),
    revokeDevice: vi.fn(),
    startMediatedPair: vi.fn(),
    cancelMediatedPair: vi.fn(),
  },
}));

const getStatus = vi.mocked(client.getStatus);
const setDeviceConfirmation = vi.mocked(client.setDeviceConfirmation);
const setDeviceRemoteSettings = vi.mocked(client.setDeviceRemoteSettings);
const revokeDevice = vi.mocked(client.revokeDevice);
const retryDevice = vi.mocked(client.retryDevice);
const startMediatedPair = vi.mocked(client.startMediatedPair);
const cancelMediatedPair = vi.mocked(client.cancelMediatedPair);

const MAC_UA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML)";
const WINDOWS_UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36";
const originalUserAgent = navigator.userAgent;

/** setUserAgent makes the platform helpers read the given user agent. */
function setUserAgent(userAgent: string): void {
  Object.defineProperty(navigator, "userAgent", { value: userAgent, configurable: true });
}

afterEach(() => {
  setUserAgent(originalUserAgent);
});

beforeEach(() => {
  vi.clearAllMocks();
  mediated.pairs = [];
  mediated.notice = null;
  mediated.answering = [];
  getStatus.mockResolvedValue(
    create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [personal("device-1"), friends("device-2")],
    }),
  );
  setDeviceConfirmation.mockResolvedValue(create(SetDeviceConfirmationResponseSchema, {}));
  setDeviceRemoteSettings.mockResolvedValue(create(SetDeviceRemoteSettingsResponseSchema, {}));
  revokeDevice.mockResolvedValue(create(RevokeDeviceResponseSchema, {}));
  retryDevice.mockResolvedValue(create(RetryDeviceResponseSchema, {}));
  startMediatedPair.mockResolvedValue(create(StartMediatedPairResponseSchema, {}));
  cancelMediatedPair.mockResolvedValue(create(CancelMediatedPairResponseSchema, {}));
  daemon.status = create(GetStatusResponseSchema, {
    running: true,
    deviceId: "self",
    deviceName: "This Mac",
    devices: [personal("device-1"), friends("device-2")],
  });
  daemon.error = null;
});

describe("devices screen", () => {
  it("offers the four confirmation flags and the remote permission to Personal devices only", () => {
    render(Devices);

    expect(screen.getAllByRole("checkbox")).toHaveLength(5);
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
    expect(
      screen.getByRole("checkbox", {
        name: "Allow MacBook to change these settings",
      }),
    ).toBeTruthy();
  });

  it("names this device after the platform", () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "Studio",
      devices: [personal("device-1")],
    });
    setUserAgent(WINDOWS_UA);

    render(Devices);

    expect(screen.getByText("This PC")).toBeTruthy();
    expect(screen.queryByText("This Mac")).toBeNull();
  });

  it("keeps the Mac name on macOS", () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "Studio",
      devices: [personal("device-1")],
    });
    setUserAgent(MAC_UA);

    render(Devices);

    expect(screen.getByText("This Mac")).toBeTruthy();
  });

  it("names this device in the confirmation lines", () => {
    render(Devices);

    expect(screen.getByText("On this device, from MacBook")).toBeTruthy();
    expect(screen.getByText("On MacBook, from this device")).toBeTruthy();
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

  it("warns about a device that speaks a newer protocol", () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [personal("device-1", { protocolNewer: true })],
    });

    render(Devices);

    expect(screen.getByText("MacBook speaks a newer protocol than this app.")).toBeTruthy();
    expect(screen.getByText("Update Unison on this device to connect to it.")).toBeTruthy();
  });

  it("keeps a device with an older protocol quiet", () => {
    render(Devices);

    expect(screen.queryByText(/speaks a newer protocol/)).toBeNull();
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

  it("shows the remote permission the device reported", () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [personal("device-1", { allowRemoteSettings: false })],
    });

    render(Devices);

    const toggle = screen.getByRole("checkbox", {
      name: "Allow MacBook to change these settings",
    });
    expect((toggle as HTMLInputElement).checked).toBe(false);
  });

  it("lets the device change these settings", async () => {
    render(Devices);
    const toggle = screen.getByRole("checkbox", {
      name: "Allow MacBook to change these settings",
    });
    expect((toggle as HTMLInputElement).checked).toBe(true);

    await fireEvent.click(toggle);

    expect(setDeviceRemoteSettings).toHaveBeenCalledWith({ deviceId: "device-1", allow: false });
  });

  it("turns the remote permission back on", async () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [personal("device-1", { allowRemoteSettings: false })],
    });

    render(Devices);

    await fireEvent.click(
      screen.getByRole("checkbox", {
        name: "Allow MacBook to change these settings",
      }),
    );

    expect(setDeviceRemoteSettings).toHaveBeenCalledWith({ deviceId: "device-1", allow: true });
  });

  it("keeps the remote permission of an offline device available", () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [personal("device-1", { online: false })],
    });

    render(Devices);

    const toggle = screen.getByRole("checkbox", {
      name: "Allow MacBook to change these settings",
    });
    expect((toggle as HTMLInputElement).disabled).toBe(false);
  });

  it("retries a device that is offline", async () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [personal("device-1", { online: false })],
    });

    render(Devices);
    await fireEvent.click(screen.getByRole("button", { name: "Reconnect" }));

    expect(retryDevice).toHaveBeenCalledWith({ deviceId: "device-1" });
    expect(await screen.findByText(/Trying to reach MacBook/)).toBeTruthy();
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

  it("keeps the name typed while the status refreshes", async () => {
    render(Devices);
    await fireEvent.click(screen.getByRole("button", { name: "Rename" }));
    const input = screen.getByLabelText("New device name");
    await fireEvent.input(input, { target: { value: "Office MacBook" } });

    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [personal("device-1"), friends("device-2")],
    });
    await tick();

    expect((input as HTMLInputElement).value).toBe("Office MacBook");
  });

  it("keeps the name typed through a lost connection", async () => {
    render(Devices);
    await fireEvent.click(screen.getByRole("button", { name: "Rename" }));
    await fireEvent.input(screen.getByLabelText("New device name"), {
      target: { value: "Office MacBook" },
    });

    daemon.status = null;
    await tick();
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [personal("device-1"), friends("device-2")],
    });
    await tick();

    expect(screen.getByLabelText<HTMLInputElement>("New device name").value).toBe("Office MacBook");
  });
});

describe("introduction of two devices", () => {
  it("offers the action when two online devices support it", () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [
        personal("device-1", { supportsMediatedPair: true }),
        personal("device-2", { supportsMediatedPair: true }),
      ],
    });

    render(Devices);

    expect(screen.getByRole("button", { name: "Introduce to another device..." })).toBeTruthy();
  });

  it("keeps the action away when the second device cannot take part", () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [personal("device-1", { supportsMediatedPair: true }), personal("device-2")],
    });

    render(Devices);

    expect(screen.queryByRole("button", { name: "Introduce to another device..." })).toBeNull();
  });

  it("locks the role to Friends when one device is a Friends device", async () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [
        personal("device-1", { supportsMediatedPair: true }),
        friends("device-2", { online: true, supportsMediatedPair: true }),
      ],
    });

    render(Devices);
    await fireEvent.click(screen.getByRole("button", { name: "Introduce to another device..." }));

    const personalRole = screen.getByRole("radio", { name: "Personal" });
    const friendsRole = screen.getByRole("radio", { name: "Friends" });
    expect((personalRole as HTMLInputElement).disabled).toBe(true);
    expect((friendsRole as HTMLInputElement).checked).toBe(true);
    expect(screen.getByText(/Friends only, because Phone is a Friends device/)).toBeTruthy();

    await fireEvent.click(screen.getByRole("button", { name: "Introduce" }));
    expect(startMediatedPair).toHaveBeenCalledWith({
      peerA: "device-1",
      peerB: "device-2",
      role: Role.FRIENDS,
    });
  });

  it("introduces the chosen device and shows the code of the exchange", async () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [
        personal("device-1", { supportsMediatedPair: true }),
        personal("device-2", { name: "Studio", supportsMediatedPair: true }),
      ],
    });
    startMediatedPair.mockResolvedValue(
      create(StartMediatedPairResponseSchema, { pair: exchange() }),
    );

    render(Devices);
    await fireEvent.click(screen.getByRole("button", { name: "Introduce to another device..." }));
    await fireEvent.click(screen.getByRole("button", { name: "Introduce" }));

    expect(startMediatedPair).toHaveBeenCalledWith({
      peerA: "device-1",
      peerB: "device-2",
      role: Role.PERSONAL,
    });
    expect(await screen.findByText("Waiting for both devices to confirm.")).toBeTruthy();
    expect(screen.getByText("123 456")).toBeTruthy();
  });

  it("introduces the device picked in the dialog", async () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [
        personal("device-1", { supportsMediatedPair: true }),
        personal("device-2", { name: "Studio", supportsMediatedPair: true }),
        personal("device-3", { name: "Office", supportsMediatedPair: true }),
      ],
    });

    render(Devices);
    await fireEvent.click(screen.getByRole("button", { name: "Introduce to another device..." }));
    await fireEvent.change(screen.getByLabelText("Second device"), {
      target: { value: "device-3" },
    });
    await fireEvent.click(screen.getByRole("button", { name: "Introduce" }));

    expect(startMediatedPair).toHaveBeenCalledWith({
      peerA: "device-1",
      peerB: "device-3",
      role: Role.PERSONAL,
    });
  });

  it("cancels a running exchange", async () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [
        personal("device-1", { supportsMediatedPair: true }),
        personal("device-2", { name: "Studio", supportsMediatedPair: true }),
      ],
    });
    startMediatedPair.mockResolvedValue(
      create(StartMediatedPairResponseSchema, { pair: exchange() }),
    );

    render(Devices);
    await fireEvent.click(screen.getByRole("button", { name: "Introduce to another device..." }));
    await fireEvent.click(screen.getByRole("button", { name: "Introduce" }));
    await fireEvent.click(await screen.findByRole("button", { name: "Cancel the introduction" }));

    expect(cancelMediatedPair).toHaveBeenCalledWith({ sessionId: "s1" });
    expect(await screen.findByText("The introduction is canceled.")).toBeTruthy();
  });

  it("shows an exchange the daemon still holds", async () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [
        personal("device-1", { supportsMediatedPair: true }),
        personal("device-2", { name: "Studio", supportsMediatedPair: true }),
      ],
    });
    mediated.pairs = [exchange()];

    render(Devices);
    await fireEvent.click(screen.getByRole("button", { name: "Introduce to another device..." }));

    expect(screen.queryByLabelText("Second device")).toBeNull();
    expect(screen.getByText("Waiting for both devices to confirm.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Cancel the introduction" })).toBeTruthy();
  });

  it("renders a verification code with repeated emoji", async () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      devices: [
        personal("device-1", { supportsMediatedPair: true }),
        personal("device-2", { name: "Studio", supportsMediatedPair: true }),
      ],
    });
    const repeated = exchange();
    repeated.sas = create(SasSchema, { digits: "654321", emoji: ["🐶", "🐶", "🐱", "🐶"] });
    mediated.pairs = [repeated];

    render(Devices);
    await fireEvent.click(screen.getByRole("button", { name: "Introduce to another device..." }));

    expect(screen.getByText("654 321")).toBeTruthy();
    expect(screen.getAllByText("🐶")).toHaveLength(3);
  });
});

/** exchange builds a running introduction of the two devices. */
function exchange() {
  return create(MediatedPairSchema, {
    sessionId: "s1",
    direction: MediatedPairDirection.MEDIATOR,
    state: MediatedPairState.WAITING,
    role: Role.PERSONAL,
    peerA: create(MediatedDeviceSchema, {
      deviceId: "device-1",
      name: "MacBook",
      platform: Platform.MACOS,
    }),
    peerB: create(MediatedDeviceSchema, {
      deviceId: "device-2",
      name: "Studio",
      platform: Platform.WINDOWS,
    }),
    sas: create(SasSchema, { digits: "123456", emoji: ["🔒", "🚀", "🌍", "🎁"] }),
  });
}

function personal(
  id: string,
  fields: {
    name?: string;
    peerRequireConfirmation?: boolean;
    peerRequireFileConfirmation?: boolean;
    allowRemoteSettings?: boolean;
    online?: boolean;
    protocolNewer?: boolean;
    supportsMediatedPair?: boolean;
  } = {},
) {
  return create(DeviceSchema, {
    deviceId: id,
    name: "MacBook",
    role: Role.PERSONAL,
    platform: Platform.MACOS,
    online: true,
    // The daemon allows the change of these settings until the user denies it.
    allowRemoteSettings: true,
    ...fields,
  });
}

function friends(id: string, fields: { online?: boolean; supportsMediatedPair?: boolean } = {}) {
  return create(DeviceSchema, {
    deviceId: id,
    name: "Phone",
    role: Role.FRIENDS,
    platform: Platform.ANDROID,
    ...fields,
  });
}
