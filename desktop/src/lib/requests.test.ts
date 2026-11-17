import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  DecideRequestResponseSchema,
  EventKind,
  EventSchema,
  ListRequestsResponseSchema,
  ShareRequestSchema,
  TransferDirection,
  TransferSchema,
  TransferState,
} from "./api/gen/unison/local/v1/local_pb";
import { client } from "./api/client";
import { requests } from "./requests.svelte";

const harness = vi.hoisted(() => {
  const handlers: Array<(event: unknown) => void> = [];
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
    listRequests: vi.fn(),
    decideRequest: vi.fn(),
  },
}));

const listRequests = vi.mocked(client.listRequests);
const decideRequest = vi.mocked(client.decideRequest);

afterEach(() => {
  requests.stop();
});

beforeEach(() => {
  requests.stop();
  vi.clearAllMocks();
  requests.requests = [];
  requests.error = null;
  requests.notice = null;
  requests.answering = [];
  harness.handlers.length = 0;
});

describe("incoming requests", () => {
  it("loads the offers that wait for an answer", async () => {
    listRequests.mockResolvedValue(
      create(ListRequestsResponseSchema, { requests: [offer("t1", "MacBook")] }),
    );

    await requests.load();

    expect(requests.requests).toHaveLength(1);
    expect(requests.pending).toBe(1);
    expect(requests.error).toBeNull();
  });

  it("reports a failure of the daemon", async () => {
    listRequests.mockRejectedValue(new Error("connection refused"));

    await requests.load();

    expect(requests.error).toBe("connection refused");
  });

  it("reloads the list when an offer arrives", () => {
    requests.start();
    listRequests.mockResolvedValue(create(ListRequestsResponseSchema, {}));

    harness.handlers[0](
      create(EventSchema, {
        kind: EventKind.TRANSFER,
        transfer: create(TransferSchema, {
          id: "t1",
          direction: TransferDirection.INCOMING,
          state: TransferState.PENDING,
        }),
      }),
    );

    expect(listRequests).toHaveBeenCalled();
  });

  it("drops an offer that ended without an answer", () => {
    requests.start();
    requests.requests = [offer("t1", "MacBook"), offer("t2", "Phone")];

    harness.handlers[0](
      create(EventSchema, {
        kind: EventKind.TRANSFER,
        transfer: create(TransferSchema, {
          id: "t1",
          direction: TransferDirection.INCOMING,
          state: TransferState.DECLINED,
        }),
      }),
    );

    expect(requests.requests.map((request) => request.id)).toEqual(["t2"]);
  });
});

describe("answers to requests", () => {
  it("accepts an offer and drops it from the list", async () => {
    decideRequest.mockResolvedValue(
      create(DecideRequestResponseSchema, { request: offer("t1", "MacBook") }),
    );
    requests.requests = [offer("t1", "MacBook")];

    await requests.decide("t1", true);

    expect(decideRequest).toHaveBeenCalledWith({ id: "t1", accept: true });
    expect(requests.requests).toEqual([]);
    expect(requests.notice?.kind).toBe("info");
  });

  it("declines an offer", async () => {
    decideRequest.mockResolvedValue(create(DecideRequestResponseSchema, {}));
    requests.requests = [offer("t1", "MacBook")];

    await requests.decide("t1", false);

    expect(decideRequest).toHaveBeenCalledWith({ id: "t1", accept: false });
    expect(requests.requests).toEqual([]);
  });

  it("keeps an offer that could not be answered", async () => {
    decideRequest.mockRejectedValue(new Error("the request has expired"));
    requests.requests = [offer("t1", "MacBook")];

    await requests.decide("t1", true);

    expect(requests.requests).toHaveLength(1);
    expect(requests.notice).toEqual({ kind: "error", text: "the request has expired" });
  });

  it("does not answer the same offer twice", async () => {
    decideRequest.mockResolvedValue(create(DecideRequestResponseSchema, {}));
    requests.answering = ["t1"];

    await requests.decide("t1", true);

    expect(decideRequest).not.toHaveBeenCalled();
  });
});

function offer(id: string, name: string) {
  return create(ShareRequestSchema, {
    id,
    deviceId: "device-1",
    deviceName: name,
    files: [{ name: "report.pdf", size: 2048n }],
    totalBytes: 2048n,
    createdAtUnixMs: 1_700_000_000_000n,
    expiresAtUnixMs: 1_700_000_600_000n,
  });
}
