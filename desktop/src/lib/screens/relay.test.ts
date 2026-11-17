import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen } from "@testing-library/svelte";
import {
  GetRelayResponseSchema,
  GetStatusResponseSchema,
  RelaySchema,
  RelayTransport,
  RelayCheckFailure,
  RelayCheckSchema,
  RelayOffResponseSchema,
  SetRelayResponseSchema,
  TestRelayResponseSchema,
} from "../api/gen/unison/local/v1/local_pb";
import { client } from "../api/client";
import { daemon } from "../daemon.svelte";
import RelaySection from "./RelaySection.svelte";

vi.mock("../api/client", () => ({
  client: {
    getRelay: vi.fn(),
    setRelay: vi.fn(),
    relayOff: vi.fn(),
    testRelay: vi.fn(),
  },
}));

const getRelay = vi.mocked(client.getRelay);
const setRelay = vi.mocked(client.setRelay);
const relayOff = vi.mocked(client.relayOff);
const testRelay = vi.mocked(client.testRelay);

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

  it("checks the entered relay without storing it", async () => {
    testRelay.mockResolvedValue(
      create(TestRelayResponseSchema, {
        quic: create(RelayCheckSchema, {
          transport: RelayTransport.QUIC,
          failure: RelayCheckFailure.TIMEOUT,
          detail: "relay: dial relay.example.org:443: timeout: no recent network activity",
        }),
        websocket: create(RelayCheckSchema, {
          transport: RelayTransport.WSS,
          ok: true,
          failure: RelayCheckFailure.NONE,
          latencyMs: 85n,
        }),
      }),
    );

    render(RelaySection);

    await fireEvent.input(await screen.findByLabelText(/Relay address/), {
      target: { value: " relay.example.org:443 " },
    });
    await fireEvent.click(screen.getByRole("button", { name: "Check connection" }));

    expect(testRelay).toHaveBeenCalledWith({ url: "relay.example.org:443", token: undefined });
    expect(setRelay).not.toHaveBeenCalled();
    expect(await screen.findByText(/The relay works over WebSocket/)).toBeTruthy();
    expect(screen.getByText("OK, 85 ms")).toBeTruthy();
    expect(screen.getByText(/No answer in time/)).toBeTruthy();
    expect(screen.getByText(/no recent network activity/)).toBeTruthy();
  });

  it("explains a token the relay refuses", async () => {
    const refused = (transport: RelayTransport) =>
      create(RelayCheckSchema, {
        transport,
        failure: RelayCheckFailure.UNAUTHORIZED,
        detail: "relay: registration refused: unauthorized",
      });
    testRelay.mockResolvedValue(
      create(TestRelayResponseSchema, {
        quic: refused(RelayTransport.QUIC),
        websocket: refused(RelayTransport.WSS),
      }),
    );

    render(RelaySection);

    await fireEvent.input(await screen.findByLabelText(/Access token/), {
      target: { value: "new-token" },
    });
    await fireEvent.click(screen.getByRole("button", { name: "Check connection" }));

    expect(testRelay).toHaveBeenCalledWith({ url: "relay.example.com:443", token: "new-token" });
    expect(await screen.findByText(/cannot be reached with these settings/)).toBeTruthy();
    expect(screen.getAllByText(/does not accept the access token/)).toHaveLength(2);
  });
});
