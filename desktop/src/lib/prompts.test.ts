import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  ClipItemSchema,
  EventKind,
  EventSchema,
  Kind,
  MediatedDeviceSchema,
  MediatedPairDirection,
  MediatedPairSchema,
  MediatedPairState,
  Platform,
  RepresentationSchema,
  Role,
  SasSchema,
  ShareRequestSchema,
  TransferFileSchema,
  type Event as DaemonEvent,
} from "./api/gen/unison/local/v1/local_pb";

const mocks = vi.hoisted(() => {
  const handlers: ((event: unknown) => void)[] = [];
  return {
    handlers,
    daemon: {
      devices: [] as { deviceId: string; name: string }[],
      onEvent: (handler: (event: unknown) => void) => {
        handlers.push(handler);
        return () => {};
      },
    },
    history: { held: [] as string[], apply: vi.fn(), release: vi.fn() },
    requests: { requests: [] as unknown[], decide: vi.fn() },
    mediated: { incoming: [] as unknown[], confirm: vi.fn(), reject: vi.fn() },
  };
});

vi.mock("./daemon.svelte", () => ({ daemon: mocks.daemon }));
vi.mock("./history.svelte", () => ({ history: mocks.history }));
vi.mock("./requests.svelte", () => ({ requests: mocks.requests }));
vi.mock("./mediated.svelte", () => ({ mediated: mocks.mediated }));

import { prompts } from "./prompts.svelte";

function emit(event: DaemonEvent): void {
  for (const handler of mocks.handlers) {
    handler(event);
  }
}

function heldEvent(id: string, name = "MacBook"): DaemonEvent {
  return create(EventSchema, {
    kind: EventKind.CLIP_APPLY,
    deviceId: "device-1",
    name,
    item: create(ClipItemSchema, {
      id,
      kind: Kind.TEXT,
      representations: [create(RepresentationSchema, { size: 2048n })],
    }),
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.history.held = [];
  mocks.requests.requests = [];
  mocks.mediated.incoming = [];
  prompts.start();
});

describe("prompt queue", () => {
  it("asks about a held clipboard item while the history holds it", () => {
    emit(heldEvent("item-1"));
    mocks.history.held = ["item-1"];

    expect(prompts.queue).toEqual([
      { id: "item-1", kind: "clipboard", device: "MacBook", summary: "Text", size: "2.0 KB" },
    ]);

    // Applied on the History screen: the question goes away.
    mocks.history.held = [];
    expect(prompts.queue).toEqual([]);
  });

  it("asks about the file offers, oldest first, after the clipboard", () => {
    emit(heldEvent("item-2"));
    mocks.history.held = ["item-2"];
    mocks.requests.requests = [
      create(ShareRequestSchema, {
        id: "newer",
        deviceName: "Pixel",
        files: [
          create(TransferFileSchema, { name: "a.txt" }),
          create(TransferFileSchema, { name: "b.txt" }),
        ],
        totalBytes: 10n,
      }),
      create(ShareRequestSchema, {
        id: "older",
        deviceName: "Pixel",
        files: [create(TransferFileSchema, { name: "report.pdf" })],
        totalBytes: 1n << 20n,
      }),
    ];

    expect(prompts.queue.map((prompt) => prompt.id)).toEqual(["item-2", "older", "newer"]);
    expect(prompts.queue[1]).toEqual({
      id: "older",
      kind: "files",
      device: "Pixel",
      summary: "report.pdf",
      size: "1.0 MB",
    });
    expect(prompts.queue[2].summary).toBe("2 files: a.txt, b.txt");
  });

  it("routes every answer to its store", async () => {
    await prompts.answer("item-3", "apply");
    await prompts.answer("item-4", "skip");
    await prompts.answer("offer-1", "accept");
    await prompts.answer("offer-2", "decline");
    await prompts.answer("offer-3", "later");

    expect(mocks.history.apply).toHaveBeenCalledWith("item-3");
    expect(mocks.history.release).toHaveBeenCalledWith("item-4");
    expect(mocks.requests.decide).toHaveBeenCalledWith("offer-1", true);
    expect(mocks.requests.decide).toHaveBeenCalledWith("offer-2", false);
    expect(mocks.requests.decide).toHaveBeenCalledTimes(2);
  });

  it("asks about an introduction after the file offers", () => {
    mocks.requests.requests = [create(ShareRequestSchema, { id: "offer-1", deviceName: "Pixel" })];
    mocks.mediated.incoming = [introduction()];

    expect(prompts.queue.map((prompt) => prompt.id)).toEqual(["offer-1", "s1"]);
    expect(prompts.queue[1]).toEqual({
      id: "s1",
      kind: "mediated",
      device: "MacBook",
      summary: "Phone",
      size: "",
      mediated: {
        platform: "Android",
        deviceId: "0123 4567 89AB",
        role: "Personal",
        digits: "123 456",
        emoji: ["🔒", "🚀", "🌍", "🎁"],
        alreadyPaired: false,
        friends: false,
      },
    });
  });

  it("routes the answer of an introduction with the friends choice", async () => {
    mocks.mediated.incoming = [introduction()];

    await prompts.answer("s1", "accept", false);
    await prompts.answer("s1", "decline");

    expect(mocks.mediated.confirm).toHaveBeenCalledWith("s1", false);
    expect(mocks.mediated.reject).toHaveBeenCalledWith("s1");
    expect(mocks.requests.decide).not.toHaveBeenCalled();
  });
});

/** introduction builds an offer another device makes to this one. */
function introduction() {
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
  });
}
