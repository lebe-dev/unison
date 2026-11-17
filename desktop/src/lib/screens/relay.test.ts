import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen } from "@testing-library/svelte";
import {
  GetRelayResponseSchema,
  GetStatusResponseSchema,
  RelaySchema,
  RelayTransport,
  RelayOffResponseSchema,
  SetRelayResponseSchema,
} from "../api/gen/unison/local/v1/local_pb";
import { client } from "../api/client";
import { daemon } from "../daemon.svelte";
import RelaySection from "./RelaySection.svelte";

vi.mock("../api/client", () => ({
  client: {
    getRelay: vi.fn(),
    setRelay: vi.fn(),
    relayOff: vi.fn(),
  },
}));

const getRelay = vi.mocked(client.getRelay);
const setRelay = vi.mocked(client.setRelay);
const relayOff = vi.mocked(client.relayOff);

beforeEach(() => {
  vi.clearAllMocks();
  getRelay.mockResolvedValue(
    create(GetRelayResponseSchema, {
      relay: create(RelaySchema, {
        url: "relay.example.com:443",
        tokenSet: true,
        filesOnRequest: true,
      }),
    }),
  );
  setRelay.mockResolvedValue(
    create(SetRelayResponseSchema, {
      relay: create(RelaySchema, {
        url: "relay.example.com:443",
        tokenSet: true,
        connected: true,
        transport: RelayTransport.QUIC,
        filesOnRequest: true,
      }),
    }),
  );
  relayOff.mockResolvedValue(create(RelayOffResponseSchema, { relay: create(RelaySchema, {}) }));
  daemon.status = create(GetStatusResponseSchema, {
    running: true,
    deviceId: "self",
    deviceName: "This Mac",
  });
  daemon.error = null;
});

describe("relay settings", () => {
  it("shows the stored relay and its transport", async () => {
    getRelay.mockResolvedValue(
      create(GetRelayResponseSchema, {
        relay: create(RelaySchema, {
          url: "relay.example.com:443",
          tokenSet: true,
          connected: true,
          transport: RelayTransport.QUIC,
          filesOnRequest: true,
        }),
      }),
    );

    render(RelaySection);

    expect(await screen.findByDisplayValue("relay.example.com:443")).toBeTruthy();
    expect(screen.getByText(/Connected to the relay over QUIC/)).toBeTruthy();
    expect(screen.getByPlaceholderText(/Stored on this device/)).toBeTruthy();
  });

  it("stores the relay without sending an empty token", async () => {
    render(RelaySection);

    await fireEvent.input(await screen.findByLabelText(/Relay address/), {
      target: { value: "relay.example.com:443" },
    });
    await fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(setRelay).toHaveBeenCalledWith({
      url: "relay.example.com:443",
      token: undefined,
      filesOnRequest: true,
    });
  });

  it("turns the relay off", async () => {
    getRelay.mockResolvedValue(
      create(GetRelayResponseSchema, {
        relay: create(RelaySchema, { url: "relay.example.com:443", tokenSet: true }),
      }),
    );

    render(RelaySection);

    await fireEvent.click(await screen.findByRole("button", { name: "Turn off" }));

    expect(relayOff).toHaveBeenCalledWith({});
  });
});
