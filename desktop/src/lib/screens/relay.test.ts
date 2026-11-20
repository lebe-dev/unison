import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen } from "@testing-library/svelte";
import { tick } from "svelte";
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
import { relayDraft } from "../relayDraft.svelte";
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

/** forgetDraft drops the values an earlier test typed into the form. */
function forgetDraft(): void {
  relayDraft.url = "";
  relayDraft.token = "";
  relayDraft.filesOnRequest = true;
  relayDraft.ready = false;
}

beforeEach(() => {
  vi.clearAllMocks();
  forgetDraft();
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

  it("warns about a relay that speaks a newer protocol", async () => {
    getRelay.mockResolvedValue(
      create(GetRelayResponseSchema, {
        relay: create(RelaySchema, {
          url: "relay.example.com:443",
          protocolNewer: true,
        }),
      }),
    );

    render(RelaySection);

    expect(await screen.findByText(/The relay speaks a newer protocol/)).toBeTruthy();
  });

  it("keeps a relay with an older protocol quiet", async () => {
    render(RelaySection);

    await screen.findByDisplayValue("relay.example.com:443");

    expect(screen.queryByText(/speaks a newer protocol/)).toBeNull();
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

describe("relay draft", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("keeps the address typed while the status refreshes", async () => {
    vi.useFakeTimers();
    const view = await renderLoaded();

    await fireEvent.input(view.getByLabelText(/Relay address/), {
      target: { value: "relay.mine.example:443" },
    });
    await vi.advanceTimersByTimeAsync(5_000);

    expect(getRelay).toHaveBeenCalledTimes(2);
    expect((view.getByLabelText(/Relay address/) as HTMLInputElement).value).toBe(
      "relay.mine.example:443",
    );
  });

  it("keeps the token typed while the status refreshes", async () => {
    vi.useFakeTimers();
    const view = await renderLoaded();

    await fireEvent.input(view.getByLabelText(/Access token/), {
      target: { value: "fresh-token" },
    });
    await vi.advanceTimersByTimeAsync(5_000);

    expect((view.getByLabelText(/Access token/) as HTMLInputElement).value).toBe("fresh-token");
  });

  it("keeps the files flag while the status refreshes", async () => {
    vi.useFakeTimers();
    const view = await renderLoaded();

    const toggle = view.getByRole("checkbox", { name: /Files over the relay only when I ask/ });
    await fireEvent.click(toggle);
    await vi.advanceTimersByTimeAsync(5_000);

    expect((toggle as HTMLInputElement).checked).toBe(false);
  });

  it("keeps the typed values when the section is mounted again", async () => {
    vi.useFakeTimers();
    const view = await renderLoaded();

    await fireEvent.input(view.getByLabelText(/Relay address/), {
      target: { value: "relay.mine.example:443" },
    });
    await fireEvent.input(view.getByLabelText(/Access token/), {
      target: { value: "fresh-token" },
    });
    view.unmount();

    const again = render(RelaySection);
    await tick();
    await vi.advanceTimersByTimeAsync(0);
    await tick();

    expect((again.getByLabelText(/Relay address/) as HTMLInputElement).value).toBe(
      "relay.mine.example:443",
    );
    expect((again.getByLabelText(/Access token/) as HTMLInputElement).value).toBe("fresh-token");
  });

  it("shows the transport reported by a later poll", async () => {
    vi.useFakeTimers();
    const view = await renderLoaded();
    getRelay.mockResolvedValue(
      create(GetRelayResponseSchema, {
        relay: create(RelaySchema, {
          url: "relay.example.com:443",
          tokenSet: true,
          connected: true,
          transport: RelayTransport.WSS,
          filesOnRequest: true,
        }),
      }),
    );

    await vi.advanceTimersByTimeAsync(5_000);

    expect(view.getByText("Connected to the relay over WebSocket.")).toBeTruthy();
  });

  it("takes the stored values after a save", async () => {
    setRelay.mockResolvedValue(
      create(SetRelayResponseSchema, {
        relay: create(RelaySchema, {
          url: "relay.saved.example:443",
          tokenSet: true,
          filesOnRequest: false,
        }),
      }),
    );
    render(RelaySection);

    const address = await screen.findByLabelText(/Relay address/);
    await fireEvent.input(address, { target: { value: "relay.saved.example:443" } });
    await fireEvent.input(screen.getByLabelText(/Access token/), {
      target: { value: "fresh-token" },
    });
    await fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect((address as HTMLInputElement).value).toBe("relay.saved.example:443");
    expect(screen.getByLabelText<HTMLInputElement>(/Access token/).value).toBe("");
    expect(
      screen.getByRole<HTMLInputElement>("checkbox", {
        name: /Files over the relay only when I ask/,
      }).checked,
    ).toBe(false);
  });
});

/** renderLoaded renders the section and waits for the first read of the relay. */
async function renderLoaded() {
  const view = render(RelaySection);
  await tick();
  await vi.advanceTimersByTimeAsync(0);
  await tick();
  return view;
}
