import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import {
  ConfirmSasResponseSchema,
  ExtendPairingWindowResponseSchema,
  ListPairingCandidatesResponseSchema,
  OpenPairingWindowResponseSchema,
  PairingCandidateSchema,
  GetPairingQrResponseSchema,
  PairingMethod,
  PairingQrSchema,
  PairingWindowSchema,
  Platform,
  RejectSasResponseSchema,
  Role,
} from "./api/gen/unison/local/v1/local_pb";

vi.mock("./api/client", () => ({
  client: {
    openPairingWindow: vi.fn(),
    extendPairingWindow: vi.fn(),
    closePairingWindow: vi.fn(),
    listPairingCandidates: vi.fn(),
    startSas: vi.fn(),
    confirmSas: vi.fn(),
    rejectSas: vi.fn(),
    getPairingQr: vi.fn(),
  },
}));

import { client } from "./api/client";
import { pairing } from "./pairing.svelte";

const mocked = vi.mocked(client);
const START = new Date("2026-01-01T12:00:00Z");

function window(expiresAt: number) {
  return create(OpenPairingWindowResponseSchema, {
    window: create(PairingWindowSchema, {
      open: true,
      addr: "192.0.2.5:5555",
      expiresAtUnixMs: BigInt(expiresAt),
    }),
  });
}

function candidates(addr: string) {
  return create(ListPairingCandidatesResponseSchema, {
    candidates: [create(PairingCandidateSchema, { addr })],
  });
}

describe("PairingStore", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(START);
    pairing.window = null;
    pairing.qr = null;
    pairing.candidates = [];
    pairing.request = null;
    pairing.confirmed = false;
    pairing.dialing = null;
    pairing.result = null;
    pairing.error = null;
    pairing.busy = false;
    pairing.role = Role.PERSONAL;
    vi.mocked(client.listPairingCandidates).mockResolvedValue(candidates("192.0.2.6:1234"));
    vi.mocked(client.getPairingQr).mockResolvedValue(
      create(GetPairingQrResponseSchema, {
        qr: create(PairingQrSchema, {
          uri: "unison://pair?v=1&id=aa&fp=bb&t=cc&a=192.0.2.5:5555&role=personal",
          expiresAtUnixMs: BigInt(START.getTime() + 180_000),
        }),
      }),
    );
  });

  afterEach(() => {
    pairing.stop();
    vi.useRealTimers();
    vi.resetAllMocks();
  });

  it("opens a window and follows the candidate list", async () => {
    const expiresAt = Date.now() + 180_000;
    mocked.openPairingWindow.mockResolvedValue(window(expiresAt));

    await pairing.open();

    expect(pairing.error).toBeNull();
    expect(pairing.window).toEqual({
      role: Role.PERSONAL,
      addr: "192.0.2.5:5555",
      expiresAt,
      openedAt: expect.any(Number),
    });
    expect(pairing.candidates.map((candidate) => candidate.addr)).toEqual(["192.0.2.6:1234"]);
    expect(client.openPairingWindow).toHaveBeenCalledWith({ role: Role.PERSONAL });
    expect(client.getPairingQr).toHaveBeenCalledTimes(1);
    expect(pairing.qr?.uri).toContain("unison://pair?");

    await vi.advanceTimersByTimeAsync(2_000);
    expect(client.listPairingCandidates).toHaveBeenCalledTimes(2);
  });

  it("reloads the code of the window", async () => {
    mocked.openPairingWindow.mockResolvedValue(window(Date.now() + 180_000));

    await pairing.open();
    expect(client.getPairingQr).toHaveBeenCalledTimes(1);

    await pairing.loadQr();
    expect(client.getPairingQr).toHaveBeenCalledTimes(2);
  });

  it("has no code without a window", async () => {
    await pairing.loadQr();

    expect(client.getPairingQr).not.toHaveBeenCalled();
    expect(pairing.qr).toBeNull();
  });

  it("closes the window when the deadline passes", async () => {
    mocked.openPairingWindow.mockResolvedValue(window(Date.now() + 180_000));

    await pairing.open();
    await vi.advanceTimersByTimeAsync(180_000);
    expect(pairing.window).not.toBeNull();

    await vi.advanceTimersByTimeAsync(1_000);
    expect(pairing.window).toBeNull();
    expect(pairing.candidates).toEqual([]);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("moves the deadline when the window is extended", async () => {
    mocked.openPairingWindow.mockResolvedValue(window(Date.now() + 60_000));
    mocked.extendPairingWindow.mockResolvedValue(
      create(ExtendPairingWindowResponseSchema, {
        window: create(PairingWindowSchema, {
          open: true,
          addr: "192.0.2.5:5555",
          expiresAtUnixMs: BigInt(START.getTime() + 60_000 + 180_000),
        }),
      }),
    );

    await pairing.open();
    await vi.advanceTimersByTimeAsync(30_000);
    await pairing.extend();

    expect(pairing.window?.expiresAt).toBe(START.getTime() + 240_000);

    await vi.advanceTimersByTimeAsync(60_000);
    expect(pairing.window).not.toBeNull();

    await vi.advanceTimersByTimeAsync(151_000);
    expect(pairing.window).toBeNull();
  });

  it("drops a window the daemon no longer has", async () => {
    mocked.openPairingWindow.mockResolvedValue(window(Date.now() + 180_000));
    mocked.extendPairingWindow.mockRejectedValue(
      new ConnectError("node: pairing window is closed", Code.FailedPrecondition),
    );

    await pairing.open();
    await pairing.extend();

    expect(pairing.window).toBeNull();
    expect(pairing.error).toBe("pairing window is closed");
  });

  it("reports an unreachable daemon", async () => {
    mocked.openPairingWindow.mockRejectedValue(new ConnectError("no socket", Code.Unavailable));

    await pairing.open();

    expect(pairing.window).toBeNull();
    expect(pairing.error).toBe("The daemon is not reachable.");
  });

  it("confirms and rejects a request", async () => {
    mocked.confirmSas.mockResolvedValue(create(ConfirmSasResponseSchema, {}));
    mocked.rejectSas.mockResolvedValue(create(RejectSasResponseSchema, {}));
    pairing.request = {
      deviceID: "aa".repeat(32),
      name: "MacBook",
      platform: Platform.MACOS,
      role: Role.PERSONAL,
      method: PairingMethod.SAS,
      sas: null,
    };

    await pairing.confirm();
    expect(client.confirmSas).toHaveBeenCalledWith({ deviceId: "aa".repeat(32) });
    expect(pairing.confirmed).toBe(true);

    await pairing.reject();
    expect(pairing.request).toBeNull();
    expect(pairing.result?.paired).toBe(false);
    expect(pairing.result?.message).toBe("Pairing was rejected.");
  });

  it("marks a device that could not be reached", async () => {
    mocked.startSas.mockRejectedValue(
      new ConnectError("pairing: no route to host", Code.Unavailable),
    );

    await pairing.pairWith("192.0.2.9:5555");

    expect(pairing.dialing).toBeNull();
    expect(pairing.error).toBe("The daemon is not reachable.");
  });
});
