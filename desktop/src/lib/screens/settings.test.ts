import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen } from "@testing-library/svelte";
import {
  FriendsDiscoverySchema,
  GetSettingsResponseSchema,
  GetStatusResponseSchema,
  SetFriendsDiscoveryResponseSchema,
  SettingsSchema,
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
  },
}));

const getStatus = vi.mocked(client.getStatus);
const getSettings = vi.mocked(client.getSettings);
const setFriendsDiscovery = vi.mocked(client.setFriendsDiscovery);

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
  getSettings.mockResolvedValue(
    create(GetSettingsResponseSchema, {
      settings: create(SettingsSchema, { deviceName: "This Mac" }),
    }),
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
    setFriendsDiscovery.mockResolvedValue(
      create(SetFriendsDiscoveryResponseSchema, {
        friendsDiscovery: create(FriendsDiscoverySchema, {}),
      }),
    );

    render(Settings);

    await fireEvent.click(screen.getByRole("checkbox", { name: /Find friends/ }));

    expect(setFriendsDiscovery).toHaveBeenCalledWith({ on: false });
  });
});
