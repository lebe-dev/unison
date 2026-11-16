import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen } from "@testing-library/svelte";
import {
  FriendsDiscoverySchema,
  GetRelayResponseSchema,
  GetSettingsResponseSchema,
  GetStatusResponseSchema,
  PolicyAction,
  PolicyCellSchema,
  PolicyRowSchema,
  RelaySchema,
  SetFriendsDiscoveryResponseSchema,
  SettingsSchema,
  UpdateSettingsResponseSchema,
  type Settings as SettingsMessage,
} from "../api/gen/unison/local/v1/local_pb";
import { client } from "../api/client";
import { daemon } from "../daemon.svelte";
import Settings from "./Settings.svelte";

vi.mock("../api/client", () => ({
  client: {
    getStatus: vi.fn(),
    getSettings: vi.fn(),
    updateSettings: vi.fn(),
    setFriendsDiscovery: vi.fn(),
    getRelay: vi.fn(),
    setRelay: vi.fn(),
    relayOff: vi.fn(),
  },
}));

const getStatus = vi.mocked(client.getStatus);
const getSettings = vi.mocked(client.getSettings);
const updateSettings = vi.mocked(client.updateSettings);
const setFriendsDiscovery = vi.mocked(client.setFriendsDiscovery);
const getRelay = vi.mocked(client.getRelay);

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
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  getStatus.mockResolvedValue(
    create(GetStatusResponseSchema, {
      running: true,
      deviceId: "self",
      deviceName: "This Mac",
      friendsDiscovery: create(FriendsDiscoverySchema, {}),
    }),
  );
  getSettings.mockResolvedValue(create(GetSettingsResponseSchema, { settings: settings() }));
  updateSettings.mockResolvedValue(
    create(UpdateSettingsResponseSchema, { settings: settings() }),
  );
  getRelay.mockResolvedValue(
    create(GetRelayResponseSchema, { relay: create(RelaySchema, {}) }),
  );
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
    getRelay.mockResolvedValue(
      create(GetRelayResponseSchema, { relay: create(RelaySchema, {}) }),
    );
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
    expect((screen.getByLabelText("Metered Text action") as HTMLSelectElement).value).toBe(
      String(PolicyAction.AUTO),
    );
    expect((screen.getByLabelText("Metered Text limit") as HTMLInputElement).value).toBe("10");
    expect((screen.getByLabelText("Roaming Images action") as HTMLSelectElement).value).toBe(
      String(PolicyAction.METADATA),
    );
    expect((screen.getByLabelText("Roaming relay") as HTMLInputElement).checked).toBe(false);
    expect((screen.getByLabelText("Relay limit") as HTMLInputElement).value).toBe("1");
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

  it("resets the policy to the defaults", async () => {
    render(Settings);

    await fireEvent.click(await screen.findByRole("button", { name: "Reset to defaults" }));

    expect(updateSettings).toHaveBeenCalledWith({ resetPolicy: true });
  });
});
