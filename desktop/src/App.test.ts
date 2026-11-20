import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { render, screen } from "@testing-library/svelte";
import {
  DeviceSchema,
  EventKind,
  EventSchema,
  GetStatusResponseSchema,
  Platform,
  Role,
} from "./lib/api/gen/unison/local/v1/local_pb";
import { daemon } from "./lib/daemon.svelte";
import App from "./App.svelte";

vi.mock("./lib/notifications.svelte", () => ({
  notifications: {
    start: vi.fn(() => new Promise<void>(() => {})),
    stop: vi.fn(),
  },
}));

vi.mock("./lib/prompt/host", () => ({
  PromptHost: class {
    start = vi.fn().mockResolvedValue(undefined);
    stop = vi.fn();
    sync = vi.fn();
  },
}));

vi.mock("./lib/sentry", () => ({
  SentrySync: class {
    sync = vi.fn().mockResolvedValue(undefined);
  },
}));

vi.mock("./lib/tray", () => ({
  onTrayAction: vi.fn().mockResolvedValue(() => {}),
}));

vi.mock("./lib/share", () => ({
  onSharedFiles: vi.fn().mockResolvedValue(() => {}),
}));

vi.mock("./lib/history.svelte", () => ({
  history: { start: vi.fn(), stop: vi.fn(), copy: vi.fn(), notice: null },
}));

vi.mock("./lib/pairing.svelte", () => ({
  pairing: {
    start: vi.fn(),
    stop: vi.fn(),
    request: null,
    busy: false,
    confirmed: false,
    confirm: vi.fn(),
    reject: vi.fn(),
    error: null,
  },
}));

vi.mock("./lib/transfers.svelte", () => ({
  transfers: { start: vi.fn(), stop: vi.fn(), offer: vi.fn(), send: vi.fn() },
}));

vi.mock("./lib/requests.svelte", () => ({
  requests: { start: vi.fn(), stop: vi.fn(), pending: 0 },
}));

vi.mock("./lib/prompts.svelte", () => ({
  prompts: { start: vi.fn(), stop: vi.fn(), queue: [], answer: vi.fn() },
}));

describe("protocol banner", () => {
  beforeEach(() => {
    daemon.status = create(GetStatusResponseSchema, { running: true });
    daemon.error = null;
    daemon.peerIncompatible = null;
  });

  afterEach(() => {
    daemon.status = null;
    daemon.error = null;
    daemon.peerIncompatible = null;
  });

  it("asks for an update when a paired device speaks a newer protocol", () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      devices: [
        create(DeviceSchema, {
          deviceId: "device-1",
          name: "Pixel",
          role: Role.PERSONAL,
          platform: Platform.ANDROID,
          protocolNewer: true,
        }),
      ],
    });

    render(App);

    expect(
      screen.getByText(
        "A device speaks a newer protocol than this app. Update Unison on this device to keep working with it.",
      ),
    ).toBeTruthy();
  });

  it("asks for an update after an incompatible peer event", () => {
    daemon.peerIncompatible = create(EventSchema, {
      kind: EventKind.PEER_INCOMPATIBLE,
      protocolNewer: true,
    });

    render(App);

    expect(screen.getByText(/speaks a newer protocol than this app/)).toBeTruthy();
  });

  it("stays quiet when the other side speaks an older protocol", () => {
    daemon.status = create(GetStatusResponseSchema, {
      running: true,
      devices: [create(DeviceSchema, { deviceId: "device-1", name: "Pixel" })],
    });
    daemon.peerIncompatible = create(EventSchema, {
      kind: EventKind.PEER_INCOMPATIBLE,
      protocolNewer: false,
    });

    render(App);

    expect(screen.queryByText(/speaks a newer protocol than this app/)).toBeNull();
  });
});
