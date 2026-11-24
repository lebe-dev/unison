import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen } from "@testing-library/svelte";
import { tick } from "svelte";
import {
  DeviceSchema,
  FriendsDiscoverySchema,
  GetRelayResponseSchema,
  ExportSettingsResponseSchema,
  GetSettingsResponseSchema,
  GetStatusResponseSchema,
  ImportSettingsResponseSchema,
  PolicyAction,
  PolicyCellSchema,
  PolicyRowSchema,
  RelaySchema,
  RenameDeviceResponseSchema,
  SetFriendsDiscoveryResponseSchema,
  SettingsSchema,
  UpdateSettingsResponseSchema,
  type Settings as SettingsMessage,
} from "../api/gen/unison/local/v1/local_pb";
import { client } from "../api/client";
import { daemon } from "../daemon.svelte";
import { dashboard, DEFAULT_DASHBOARD_LIMIT } from "../dashboard.svelte";
import { pickFolder } from "../files";
import { relayDraft } from "../relayDraft.svelte";
import { setSentryEnabled } from "../sentry";
import {
  pickSettingsSource,
  pickSettingsTarget,
  readSettingsFile,
  writeSettingsFile,
} from "../settingsFileIO";
import Settings from "./Settings.svelte";

vi.mock("../files", () => ({
  pickFiles: vi.fn(),
  pickFolder: vi.fn(),
}));

vi.mock("../sentry", () => ({
  setSentryEnabled: vi.fn(),
}));

vi.mock("../login", () => ({
  loginItemStatus: vi.fn().mockResolvedValue({ status: "enabled" }),
  setLoginItem: vi.fn((enabled: boolean) =>
    Promise.resolve({ status: enabled ? "enabled" : "not_registered" }),
  ),
}));

vi.mock("../settingsFileIO", () => ({
  pickSettingsSource: vi.fn(),
  pickSettingsTarget: vi.fn(),
  readSettingsFile: vi.fn(),
  writeSettingsFile: vi.fn(),
}));

vi.mock("../journalIO", () => ({
  copyText: vi.fn(),
  journalEnabled: vi.fn().mockResolvedValue(false),
  readAllLines: vi.fn().mockResolvedValue([]),
  readLines: vi.fn().mockResolvedValue([]),
  recordFailure: vi.fn(),
  saveLog: vi.fn(),
  shareLog: vi.fn(),
  shellInfo: vi.fn().mockResolvedValue({ appVersion: "0.1.0", os: "macOS 14.5" }),
  writeLog: vi.fn(),
}));

const pickFolderMock = vi.mocked(pickFolder);
const setSentryEnabledMock = vi.mocked(setSentryEnabled);
const pickSettingsTargetMock = vi.mocked(pickSettingsTarget);
const pickSettingsSourceMock = vi.mocked(pickSettingsSource);
const readSettingsFileMock = vi.mocked(readSettingsFile);
const writeSettingsFileMock = vi.mocked(writeSettingsFile);

vi.mock("../api/client", () => ({
  client: {
    getStatus: vi.fn(),
    getSettings: vi.fn(),
    updateSettings: vi.fn(),
    renameDevice: vi.fn(),
    setFriendsDiscovery: vi.fn(),
    getRelay: vi.fn(),
    setRelay: vi.fn(),
    relayOff: vi.fn(),
    exportSettings: vi.fn(),
    importSettings: vi.fn(),
  },
}));

const getStatus = vi.mocked(client.getStatus);
const getSettings = vi.mocked(client.getSettings);
const updateSettings = vi.mocked(client.updateSettings);
const renameDevice = vi.mocked(client.renameDevice);
const setFriendsDiscovery = vi.mocked(client.setFriendsDiscovery);
const getRelay = vi.mocked(client.getRelay);
const exportSettings = vi.mocked(client.exportSettings);
const importSettings = vi.mocked(client.importSettings);

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

const megabyte = 1n << 20n;

/** Settings with the default policy table. */
function settings(): SettingsMessage {
  const cell = (action: PolicyAction, megabytes: number) =>
    create(PolicyCellSchema, { action, maxSizeBytes: BigInt(megabytes) * megabyte });
  const row = (text: PolicyAction, image: PolicyAction, files: PolicyAction, relay: boolean) =>
    create(PolicyRowSchema, {
      text: cell(text, 10),
      image: cell(image, 10),
      files: cell(files, 10),
      relay,
    });
  return create(SettingsSchema, {
    deviceName: "This Mac",
    unmetered: row(PolicyAction.AUTO, PolicyAction.AUTO, PolicyAction.METADATA, true),
    metered: row(PolicyAction.AUTO, PolicyAction.AUTO, PolicyAction.METADATA, true),
    roaming: row(PolicyAction.AUTO, PolicyAction.METADATA, PolicyAction.METADATA, false),
    dataSaver: row(PolicyAction.AUTO, PolicyAction.METADATA, PolicyAction.METADATA, false),
    relayMaxSizeBytes: megabyte,
    historyTtlTextSeconds: 7n * 86400n,
    historyTtlImageSeconds: 3n * 86400n,
    historyTtlFilesSeconds: 86400n,
    downloadsDir: "/Users/me/Downloads/Unison",
    sentryEnabled: false,
    stripTracking: true,
    allowRemoteSettings: true,
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  setSentryEnabledMock.mockResolvedValue();
  relayDraft.url = "";
  relayDraft.token = "";
  relayDraft.filesOnRequest = true;
  relayDraft.ready = false;
  getStatus.mockResolvedValue(
    create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      friendsDiscovery: create(FriendsDiscoverySchema, {}),
    }),
  );
  getSettings.mockResolvedValue(create(GetSettingsResponseSchema, { settings: settings() }));
  updateSettings.mockResolvedValue(create(UpdateSettingsResponseSchema, { settings: settings() }));
  renameDevice.mockResolvedValue(
    create(RenameDeviceResponseSchema, {
      device: create(DeviceSchema, { deviceId: "self", name: "This Mac" }),
    }),
  );
  getRelay.mockResolvedValue(create(GetRelayResponseSchema, { relay: create(RelaySchema, {}) }));
  setFriendsDiscovery.mockResolvedValue(
    create(SetFriendsDiscoveryResponseSchema, {
      friendsDiscovery: create(FriendsDiscoverySchema, {
        on: true,
        untilUnixMs: BigInt(Date.now() + 15 * 60_000),
      }),
    }),
  );
  daemon.status = create(GetStatusResponseSchema, {
    running: true,
    deviceId: "self",
    deviceName: "This Mac",
    friendsDiscovery: create(FriendsDiscoverySchema, {}),
  });
  daemon.error = null;
});

describe("settings screen", () => {
  it("shows the folder of the received files", async () => {
    render(Settings);

    expect(await screen.findByText("/Users/me/Downloads/Unison")).toBeTruthy();
  });

  it("names this device after the platform", async () => {
    setUserAgent(WINDOWS_UA);

    render(Settings);

    expect(await screen.findByText("This PC")).toBeTruthy();
    expect(screen.queryByText("This Mac")).toBeNull();
  });

  it("keeps the Mac name on macOS", async () => {
    setUserAgent(MAC_UA);

    render(Settings);

    expect(await screen.findByText("This Mac")).toBeTruthy();
  });

  it("stores the number of items of the dashboard", async () => {
    localStorage.removeItem("unison.dashboard.limit");
    dashboard.limit = DEFAULT_DASHBOARD_LIMIT;
    render(Settings);

    await fireEvent.change(await screen.findByLabelText("Items in each tab"), {
      target: { value: "24" },
    });

    expect(dashboard.limit).toBe(24);
    expect(localStorage.getItem("unison.dashboard.limit")).toBe("24");
  });

  it("saves the folder the user picked", async () => {
    pickFolderMock.mockResolvedValue("/Volumes/Data/Inbox");
    render(Settings);

    await fireEvent.click(await screen.findByRole("button", { name: "Choose folder" }));

    expect(updateSettings).toHaveBeenCalledWith({ downloadsDir: "/Volumes/Data/Inbox" });
  });

  it("keeps the folder when the dialog is closed", async () => {
    pickFolderMock.mockResolvedValue(null);
    render(Settings);

    await fireEvent.click(await screen.findByRole("button", { name: "Choose folder" }));

    expect(updateSettings).not.toHaveBeenCalled();
  });

  it("opens the application log page", async () => {
    render(Settings);

    await fireEvent.click(await screen.findByRole("button", { name: /Application log/ }));

    expect(await screen.findByRole("button", { name: "Back" })).toBeTruthy();
    expect(screen.getByRole("checkbox", { name: /Record the application log/ })).toBeTruthy();

    await fireEvent.click(screen.getByRole("button", { name: "Back" }));

    expect(await screen.findByText("Network policy")).toBeTruthy();
  });

  it("goes back to the default folder", async () => {
    render(Settings);

    await fireEvent.click(await screen.findByRole("button", { name: "Use default folder" }));

    expect(updateSettings).toHaveBeenCalledWith({ downloadsDir: "" });
  });

  it("turns the search for friends on", async () => {
    render(Settings);

    const toggle = screen.getByRole("checkbox", { name: /Find friends/ });
    expect((toggle as HTMLInputElement).checked).toBe(false);
    expect(screen.getByText(/Both devices have to turn it on/)).toBeTruthy();

    await fireEvent.click(toggle);

    expect(setFriendsDiscovery).toHaveBeenCalledWith({ on: true });
    expect(getStatus).toHaveBeenCalled();
  });

  it("shows the remaining time of a running search", () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      friendsDiscovery: create(FriendsDiscoverySchema, {
        on: true,
        untilUnixMs: BigInt(Date.now() + 5 * 60_000),
      }),
    });

    render(Settings);

    const toggle = screen.getByRole("checkbox", { name: /Find friends/ });
    expect((toggle as HTMLInputElement).checked).toBe(true);
    expect(screen.getByText(/Searching for \d+:\d\d/)).toBeTruthy();
    expect(screen.getByText(/Turn it on on the other device too/)).toBeTruthy();
  });

  it("turns the running search off", async () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      friendsDiscovery: create(FriendsDiscoverySchema, {
        on: true,
        untilUnixMs: BigInt(Date.now() + 60_000),
      }),
    });
    getRelay.mockResolvedValue(create(GetRelayResponseSchema, { relay: create(RelaySchema, {}) }));
    setFriendsDiscovery.mockResolvedValue(
      create(SetFriendsDiscoveryResponseSchema, {
        friendsDiscovery: create(FriendsDiscoverySchema, {}),
      }),
    );

    render(Settings);

    await fireEvent.click(screen.getByRole("checkbox", { name: /Find friends/ }));

    expect(setFriendsDiscovery).toHaveBeenCalledWith({ on: false });
  });

  it("shows the policy table of the daemon", async () => {
    render(Settings);

    expect(await screen.findByText("Network policy")).toBeTruthy();
    expect(screen.getByLabelText<HTMLSelectElement>("Metered Text action").value).toBe(
      String(PolicyAction.AUTO),
    );
    expect(screen.getByLabelText<HTMLInputElement>("Metered Text limit").value).toBe("10");
    expect(screen.getByLabelText<HTMLSelectElement>("Roaming Images action").value).toBe(
      String(PolicyAction.METADATA),
    );
    expect(screen.getByLabelText<HTMLInputElement>("Roaming relay").checked).toBe(false);
    expect(screen.getByLabelText<HTMLInputElement>("Relay limit").value).toBe("1");
  });

  it("saves a new threshold with the whole table", async () => {
    render(Settings);

    await fireEvent.change(await screen.findByLabelText("Metered Text limit"), {
      target: { value: "4" },
    });

    const request = updateSettings.mock.calls.at(-1)?.[0];
    expect(request?.metered?.text?.maxSizeBytes).toBe(4n * megabyte);
    expect(request?.unmetered?.text?.action).toBe(PolicyAction.AUTO);
    expect(request?.roaming?.relay).toBe(false);
    expect(request?.relayMaxSizeBytes).toBe(megabyte);
  });

  it("changes the action of a cell", async () => {
    render(Settings);

    await fireEvent.change(await screen.findByLabelText("Roaming Images action"), {
      target: { value: String(PolicyAction.AUTO) },
    });

    const request = updateSettings.mock.calls.at(-1)?.[0];
    expect(request?.roaming?.image?.action).toBe(PolicyAction.AUTO);
  });

  it("shows the lifetimes of the history", async () => {
    render(Settings);

    expect((await screen.findByLabelText<HTMLInputElement>("Text lifetime")).value).toBe("7d");
    expect(screen.getByLabelText<HTMLInputElement>("Images lifetime").value).toBe("3d");
    expect(screen.getByLabelText<HTMLInputElement>("Files lifetime").value).toBe("1d");
  });

  it("saves a new lifetime", async () => {
    render(Settings);

    await fireEvent.change(await screen.findByLabelText("Text lifetime"), {
      target: { value: "30d" },
    });

    expect(updateSettings).toHaveBeenLastCalledWith({ historyTtlTextSeconds: 30n * 86400n });
  });

  it("refuses an unusable lifetime", async () => {
    render(Settings);

    await fireEvent.change(await screen.findByLabelText("Images lifetime"), {
      target: { value: "soon" },
    });

    expect(screen.getByText(/Use a lifetime like/)).toBeTruthy();
    expect(updateSettings).not.toHaveBeenCalled();
  });

  it("resets the lifetimes to the defaults", async () => {
    render(Settings);

    await fireEvent.click(await screen.findByRole("button", { name: "Reset lifetimes" }));

    expect(updateSettings).toHaveBeenCalledWith({ resetHistoryTtl: true });
  });

  it("resets the policy to the defaults", async () => {
    render(Settings);

    await fireEvent.click(await screen.findByRole("button", { name: "Reset to defaults" }));

    expect(updateSettings).toHaveBeenCalledWith({ resetPolicy: true });
  });

  it("renames this device", async () => {
    render(Settings);

    await fireEvent.input(await screen.findByLabelText(/Device name/), {
      target: { value: "Kitchen Mac" },
    });
    await fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0]);

    expect(renameDevice).toHaveBeenCalledWith({ deviceId: "self", name: "Kitchen Mac" });
    expect(await screen.findByText(/announced to the paired devices/)).toBeTruthy();
  });

  it("asks before taking files from every device", async () => {
    getSettings.mockResolvedValue(
      create(GetSettingsResponseSchema, {
        settings: { ...settings(), requireConfirmation: true },
      }),
    );
    updateSettings.mockResolvedValue(
      create(UpdateSettingsResponseSchema, {
        settings: { ...settings(), requireConfirmation: true, requireFileConfirmation: true },
      }),
    );
    render(Settings);

    const clipboard = await screen.findByRole("checkbox", {
      name: /Ask before applying the clipboard/,
    });
    const files = screen.getByRole("checkbox", { name: /Ask before accepting files/ });
    await screen.findByDisplayValue("This Mac");
    expect((clipboard as HTMLInputElement).checked).toBe(true);
    expect((files as HTMLInputElement).checked).toBe(false);

    await fireEvent.click(files);

    expect(updateSettings).toHaveBeenCalledWith({ requireFileConfirmation: true });
    expect((files as HTMLInputElement).checked).toBe(true);
  });

  it("allows the paired devices to change these settings by default", async () => {
    updateSettings.mockResolvedValue(
      create(UpdateSettingsResponseSchema, {
        settings: { ...settings(), allowRemoteSettings: false },
      }),
    );
    render(Settings);

    const toggle = await screen.findByRole("checkbox", {
      name: "Allow paired devices to change these settings",
    });
    await screen.findByDisplayValue("This Mac");
    expect((toggle as HTMLInputElement).checked).toBe(true);

    await fireEvent.click(toggle);

    expect(updateSettings).toHaveBeenCalledWith({ allowRemoteSettings: false });
    expect((toggle as HTMLInputElement).checked).toBe(false);
  });

  it("reflects a refused remote settings switch", async () => {
    getSettings.mockResolvedValue(
      create(GetSettingsResponseSchema, {
        settings: { ...settings(), allowRemoteSettings: false },
      }),
    );
    render(Settings);

    const toggle = await screen.findByRole("checkbox", {
      name: "Allow paired devices to change these settings",
    });
    await screen.findByDisplayValue("This Mac");
    expect((toggle as HTMLInputElement).checked).toBe(false);

    await fireEvent.click(toggle);

    expect(updateSettings).toHaveBeenCalledWith({ allowRemoteSettings: true });
    expect((toggle as HTMLInputElement).checked).toBe(true);
  });

  it("keeps error reporting off by default", async () => {
    render(Settings);

    const toggle = await screen.findByRole("checkbox", { name: /Send errors to the developer/ });

    expect((toggle as HTMLInputElement).checked).toBe(false);
    expect(setSentryEnabled).toHaveBeenCalledWith(false);
  });

  it("passes a granted error reporting to the shell", async () => {
    getSettings.mockResolvedValue(
      create(GetSettingsResponseSchema, {
        settings: { ...settings(), sentryEnabled: true },
      }),
    );
    render(Settings);

    const toggle = await screen.findByRole("checkbox", { name: /Send errors to the developer/ });

    expect((toggle as HTMLInputElement).checked).toBe(true);
    expect(setSentryEnabled).toHaveBeenCalledWith(true);
  });

  it("turns error reporting on", async () => {
    updateSettings.mockResolvedValue(
      create(UpdateSettingsResponseSchema, {
        settings: { ...settings(), sentryEnabled: true },
      }),
    );
    render(Settings);

    const toggle = await screen.findByRole("checkbox", { name: /Send errors to the developer/ });
    await fireEvent.click(toggle);

    expect(updateSettings).toHaveBeenCalledWith({ sentryEnabled: true });
    expect(setSentryEnabled).toHaveBeenCalledWith(true);
    expect((toggle as HTMLInputElement).checked).toBe(true);
  });

  it("reverts the error reporting when it cannot be saved", async () => {
    updateSettings.mockRejectedValue(new Error("the daemon is busy"));
    render(Settings);

    const toggle = await screen.findByRole("checkbox", { name: /Send errors to the developer/ });
    await fireEvent.click(toggle);

    expect(await screen.findByText("the daemon is busy")).toBeTruthy();
    expect((toggle as HTMLInputElement).checked).toBe(false);
    expect(setSentryEnabled).not.toHaveBeenCalledWith(true);
  });

  it("removes the tracking parameters by default", async () => {
    render(Settings);

    const toggle = await screen.findByRole("checkbox", { name: /Remove tracking parameters/ });

    expect((toggle as HTMLInputElement).checked).toBe(true);
  });

  it("turns the removal of tracking parameters on", async () => {
    getSettings.mockResolvedValue(
      create(GetSettingsResponseSchema, {
        settings: { ...settings(), stripTracking: false },
      }),
    );
    render(Settings);

    const toggle = await screen.findByRole("checkbox", { name: /Remove tracking parameters/ });
    await screen.findByDisplayValue("This Mac");
    expect((toggle as HTMLInputElement).checked).toBe(false);

    await fireEvent.click(toggle);

    expect(updateSettings).toHaveBeenCalledWith({ stripTracking: true });
    expect((toggle as HTMLInputElement).checked).toBe(true);
  });

  it("reverts the removal of tracking parameters when it cannot be saved", async () => {
    getSettings.mockResolvedValue(
      create(GetSettingsResponseSchema, {
        settings: { ...settings(), stripTracking: false },
      }),
    );
    updateSettings.mockRejectedValue(new Error("the daemon is busy"));
    render(Settings);

    const toggle = await screen.findByRole("checkbox", { name: /Remove tracking parameters/ });
    await screen.findByDisplayValue("This Mac");
    await fireEvent.click(toggle);

    expect(await screen.findByText("the daemon is busy")).toBeTruthy();
    expect((toggle as HTMLInputElement).checked).toBe(false);
  });

  it("refuses an empty device name", async () => {
    render(Settings);

    await fireEvent.input(await screen.findByLabelText(/Device name/), {
      target: { value: "  " },
    });
    await fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0]);

    expect(screen.getByText(/cannot be empty/)).toBeTruthy();
    expect(renameDevice).not.toHaveBeenCalled();
  });

  it("keeps the device name typed while the status refreshes", async () => {
    render(Settings);
    const input = await screen.findByLabelText(/Device name/);
    await screen.findByDisplayValue("This Mac");
    await fireEvent.input(input, { target: { value: "Kitchen Mac" } });

    daemon.status = connectedStatus();
    await tick();

    expect((input as HTMLInputElement).value).toBe("Kitchen Mac");
  });

  it("keeps the device name typed through a lost connection", async () => {
    render(Settings);
    await screen.findByDisplayValue("This Mac");
    await fireEvent.input(screen.getByLabelText(/Device name/), {
      target: { value: "Kitchen Mac" },
    });

    daemon.status = null;
    await tick();
    daemon.status = connectedStatus();
    await tick();

    expect(screen.getByLabelText<HTMLInputElement>(/Device name/).value).toBe("Kitchen Mac");
  });

  it("keeps the relay address typed through a lost connection", async () => {
    getRelay.mockResolvedValue(
      create(GetRelayResponseSchema, {
        relay: create(RelaySchema, { url: "relay.example.com:443", tokenSet: true }),
      }),
    );
    render(Settings);
    await fireEvent.input(await screen.findByLabelText(/Relay address/), {
      target: { value: "relay.mine.example:443" },
    });

    daemon.status = null;
    await tick();
    daemon.status = connectedStatus();
    await tick();

    expect(await screen.findByDisplayValue("relay.mine.example:443")).toBeTruthy();
  });

  it("keeps the lifetime typed while the status refreshes", async () => {
    render(Settings);
    const input = await screen.findByLabelText("Text lifetime");
    await fireEvent.input(input, { target: { value: "30d" } });

    daemon.status = connectedStatus();
    await tick();

    expect((input as HTMLInputElement).value).toBe("30d");
  });
});

describe("settings file", () => {
  it("saves the settings in the file the user picked", async () => {
    exportSettings.mockResolvedValue(
      create(ExportSettingsResponseSchema, {
        document: new TextEncoder().encode('{"schema":"unison-settings"}'),
        fileName: "unison-macos-20261124-102030.json",
      }),
    );
    pickSettingsTargetMock.mockResolvedValue("/tmp/unison-settings.json");
    writeSettingsFileMock.mockResolvedValue();
    render(Settings);

    await fireEvent.click(await screen.findByRole("button", { name: "Export" }));

    const request = exportSettings.mock.calls[0]?.[0];
    expect(request?.appSettings).toContain('"desktop"');
    expect(pickSettingsTargetMock).toHaveBeenCalledWith("unison-macos-20261124-102030.json");
    expect(writeSettingsFileMock).toHaveBeenCalledWith(
      "/tmp/unison-settings.json",
      '{"schema":"unison-settings"}',
    );
    expect(await screen.findByText(/saved to \/tmp\/unison-settings.json/)).toBeTruthy();
  });

  it("writes nothing when the save dialog is closed", async () => {
    exportSettings.mockResolvedValue(
      create(ExportSettingsResponseSchema, {
        document: new TextEncoder().encode("{}"),
        fileName: "x.json",
      }),
    );
    pickSettingsTargetMock.mockResolvedValue(null);
    render(Settings);

    await fireEvent.click(await screen.findByRole("button", { name: "Export" }));

    expect(writeSettingsFileMock).not.toHaveBeenCalled();
  });

  it("applies the settings of the file the user picked", async () => {
    const text = JSON.stringify({
      schema: "unison-settings",
      format_version: 1,
      core: { strip_tracking: false },
      app: { desktop: { theme: "dark", login_item: false } },
    });
    pickSettingsSourceMock.mockResolvedValue("/tmp/unison-settings.json");
    readSettingsFileMock.mockResolvedValue(text);
    importSettings.mockResolvedValue(
      create(ImportSettingsResponseSchema, { settings: settings() }),
    );
    render(Settings);

    await fireEvent.click(await screen.findByRole("button", { name: "Import" }));

    expect(importSettings).toHaveBeenCalledWith({ document: new TextEncoder().encode(text) });
    expect(await screen.findByText("The settings are imported.")).toBeTruthy();
  });

  it("reports the notes of a document of a newer app", async () => {
    pickSettingsSourceMock.mockResolvedValue("/tmp/unison-settings.json");
    readSettingsFileMock.mockResolvedValue('{"schema":"unison-settings","format_version":99}');
    importSettings.mockResolvedValue(
      create(ImportSettingsResponseSchema, {
        settings: settings(),
        fromNewerVersion: true,
        ignored: ["core.future_setting"],
      }),
    );
    render(Settings);

    await fireEvent.click(await screen.findByRole("button", { name: "Import" }));

    expect(await screen.findByText(/newer version of the app/)).toBeTruthy();
    expect(await screen.findByText(/core.future_setting/)).toBeTruthy();
  });

  it("imports nothing when the open dialog is closed", async () => {
    pickSettingsSourceMock.mockResolvedValue(null);
    render(Settings);

    await fireEvent.click(await screen.findByRole("button", { name: "Import" }));

    expect(importSettings).not.toHaveBeenCalled();
  });
});

/** connectedStatus returns the status of a running daemon. */
function connectedStatus() {
  return create(GetStatusResponseSchema, {
    running: true,
    deviceId: "self",
    deviceName: "This Mac",
    friendsDiscovery: create(FriendsDiscoverySchema, {}),
  });
}
