import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  ConfirmMediatedPairResponseSchema,
  EventKind,
  EventSchema,
  MediatedDeviceSchema,
  MediatedPairCancelReason,
  MediatedPairDirection,
  MediatedPairSchema,
  MediatedPairState,
  Platform,
  RejectMediatedPairResponseSchema,
  Role,
  SasSchema,
  StartMediatedPairResponseSchema,
  type Event as DaemonEvent,
  type MediatedPair,
} from "./api/gen/unison/local/v1/local_pb";

const harness = vi.hoisted(() => {
  const handlers: ((event: unknown) => void)[] = [];
  return {
    handlers,
    onEvent(handler: (event: unknown) => void) {
      handlers.push(handler);
      return () => {
        handlers.length = 0;
      };
    },
  };
});

vi.mock("./daemon.svelte", () => ({
  daemon: { onEvent: harness.onEvent },
}));

vi.mock("./api/client", () => ({
  client: {
    listMediatedPairs: vi.fn(),
    startMediatedPair: vi.fn(),
    confirmMediatedPair: vi.fn(),
    rejectMediatedPair: vi.fn(),
    cancelMediatedPair: vi.fn(),
  },
}));

import { client } from "./api/client";
import { mediated } from "./mediated.svelte";
import { roleLimit } from "./mediated";

const startMediatedPair = vi.mocked(client.startMediatedPair);
const confirmMediatedPair = vi.mocked(client.confirmMediatedPair);
const rejectMediatedPair = vi.mocked(client.rejectMediatedPair);

function emit(kind: EventKind, pair: MediatedPair): void {
  const event: DaemonEvent = create(EventSchema, { kind, mediatedPair: pair });
  for (const handler of harness.handlers) {
    handler(event);
  }
}

function introduction(
  fields: Partial<Omit<MediatedPair, "$typeName" | "$unknown">> = {},
): MediatedPair {
  return create(MediatedPairSchema, {
    sessionId: "s1",
    direction: MediatedPairDirection.INCOMING,
    state: MediatedPairState.WAITING,
    role: Role.PERSONAL,
    mediator: create(MediatedDeviceSchema, {
      deviceId: "mediator",
      name: "MacBook",
      platform: Platform.MACOS,
    }),
    peer: create(MediatedDeviceSchema, {
      deviceId: "0123456789ab",
      name: "Phone",
      platform: Platform.ANDROID,
    }),
    sas: create(SasSchema, { digits: "123456", emoji: ["🔒", "🚀", "🌍", "🎁"] }),
    expiresAtUnixMs: 1_700_000_000_000n,
    ...fields,
  });
}

beforeEach(() => {
  mediated.stop();
  vi.clearAllMocks();
  mediated.pairs = [];
  mediated.error = null;
  mediated.notice = null;
  mediated.answering = [];
  harness.handlers.length = 0;
});

afterEach(() => {
  mediated.stop();
});

describe("role of an introduced pair", () => {
  it("stays Personal when both devices are Personal", () => {
    expect(roleLimit(Role.PERSONAL, Role.PERSONAL)).toBe(Role.PERSONAL);
  });

  it("falls back to Friends when one device is Friends", () => {
    expect(roleLimit(Role.PERSONAL, Role.FRIENDS)).toBe(Role.FRIENDS);
    expect(roleLimit(Role.FRIENDS, Role.PERSONAL)).toBe(Role.FRIENDS);
    expect(roleLimit(Role.FRIENDS, Role.FRIENDS)).toBe(Role.FRIENDS);
  });

  it("fails closed on an unknown role", () => {
    expect(roleLimit(Role.UNSPECIFIED, Role.PERSONAL)).toBe(Role.FRIENDS);
  });
});

describe("introductions of the daemon", () => {
  it("lists an offer that another device makes", () => {
    mediated.start();

    emit(EventKind.MEDIATED_PAIR_OFFER, introduction());

    expect(mediated.incoming.map((pair) => pair.sessionId)).toEqual(["s1"]);
    expect(mediated.pending).toBe(1);
  });

  it("keeps an answered offer out of the waiting list", () => {
    mediated.start();
    emit(EventKind.MEDIATED_PAIR_OFFER, introduction());

    emit(
      EventKind.MEDIATED_PAIR_UPDATED,
      introduction({ state: MediatedPairState.AWAITING_FINISH }),
    );

    expect(mediated.incoming).toEqual([]);
    expect(mediated.pairs).toHaveLength(1);
  });

  it("reports the device an introduction added", () => {
    mediated.start();
    emit(EventKind.MEDIATED_PAIR_OFFER, introduction());

    emit(EventKind.MEDIATED_PAIR_COMPLETED, introduction({ state: MediatedPairState.COMPLETED }));

    expect(mediated.pairs).toEqual([]);
    expect(mediated.notice).toEqual({ kind: "info", text: "Phone is paired." });
  });

  it("reports why an introduction ended", () => {
    mediated.start();

    emit(
      EventKind.MEDIATED_PAIR_CANCELED,
      introduction({
        state: MediatedPairState.CANCELED,
        cancelReason: MediatedPairCancelReason.EXPIRED,
      }),
    );

    expect(mediated.notice).toEqual({
      kind: "error",
      text: "The introduction ended: the offer expired.",
    });
  });

  it("keeps quiet about an exchange the user canceled", () => {
    mediated.start();

    emit(
      EventKind.MEDIATED_PAIR_CANCELED,
      introduction({
        state: MediatedPairState.CANCELED,
        cancelReason: MediatedPairCancelReason.MEDIATOR_CANCELED,
      }),
    );

    expect(mediated.notice).toBeNull();
  });

  it("ignores events of other kinds", () => {
    mediated.start();

    emit(EventKind.PEER_ONLINE, introduction());

    expect(mediated.pairs).toEqual([]);
  });
});

describe("actions of an introduction", () => {
  it("starts an exchange between the chosen devices", async () => {
    startMediatedPair.mockResolvedValue(
      create(StartMediatedPairResponseSchema, {
        pair: introduction({
          direction: MediatedPairDirection.MEDIATOR,
          peerA: create(MediatedDeviceSchema, { deviceId: "a", name: "Phone" }),
          peerB: create(MediatedDeviceSchema, { deviceId: "b", name: "Studio" }),
        }),
      }),
    );

    const sessionId = await mediated.startPair("a", "b", Role.FRIENDS);

    expect(sessionId).toBe("s1");
    expect(startMediatedPair).toHaveBeenCalledWith({
      peerA: "a",
      peerB: "b",
      role: Role.FRIENDS,
    });
    expect(mediated.outgoing).toHaveLength(1);
  });

  it("reports an exchange the daemon refused", async () => {
    startMediatedPair.mockRejectedValue(new Error("both devices are online"));

    expect(await mediated.startPair("a", "b", Role.PERSONAL)).toBeNull();
    expect(mediated.outgoing).toEqual([]);
    expect(mediated.notice).toEqual({ kind: "error", text: "both devices are online" });
  });

  it("passes the choice of the friends search on the confirmation", async () => {
    mediated.pairs = [introduction()];
    confirmMediatedPair.mockResolvedValue(
      create(ConfirmMediatedPairResponseSchema, {
        pair: introduction({ state: MediatedPairState.READY }),
      }),
    );

    await mediated.confirm("s1", false);

    expect(confirmMediatedPair).toHaveBeenCalledWith({
      sessionId: "s1",
      enableFriendsDiscovery: false,
    });
    expect(mediated.notice?.kind).toBe("info");
  });

  it("declines an offer and drops it", async () => {
    mediated.pairs = [introduction()];
    rejectMediatedPair.mockResolvedValue(
      create(RejectMediatedPairResponseSchema, { pair: introduction() }),
    );

    await mediated.reject("s1");

    expect(rejectMediatedPair).toHaveBeenCalledWith({ sessionId: "s1" });
    expect(mediated.pairs).toEqual([]);
    expect(mediated.notice).toEqual({ kind: "info", text: "The introduction is declined." });
  });
});
