import { beforeEach, describe, expect, it, vi } from "vitest";
import { create, fromBinary, toBinary } from "@bufbuild/protobuf";

const mocks = vi.hoisted(() => {
  class Channel<T> {
    onmessage: (message: T) => void = () => {};
  }
  return { invoke: vi.fn(), Channel };
});

vi.mock("@tauri-apps/api/core", () => ({ invoke: mocks.invoke, Channel: mocks.Channel }));

import { client, invokeFetch } from "./client";
import {
  EventKind,
  EventSchema,
  GetStatusRequestSchema,
  GetStatusResponseSchema,
  SetClipRequestSchema,
  WatchEventsResponseSchema,
} from "./gen/unison/local/v1/local_pb";

interface StreamCall {
  id: number;
  service: string;
  method: string;
  body: string;
  onChunk: { onmessage: (message: { chunk: string }) => void };
  resolve: () => void;
  reject: (error: unknown) => void;
}

function toBase64(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString("base64");
}

function fromBase64(value: string): Uint8Array {
  return new Uint8Array(Buffer.from(value, "base64"));
}

/** envelope frames one message of a Connect stream. */
function envelope(flags: number, payload: Uint8Array): Uint8Array {
  const frame = new Uint8Array(5 + payload.length);
  frame[0] = flags;
  new DataView(frame.buffer).setUint32(1, payload.length);
  frame.set(payload, 5);
  return frame;
}

function eventFrame(kind: EventKind, deviceId: string): Uint8Array {
  const message = create(WatchEventsResponseSchema, {
    event: create(EventSchema, { kind, deviceId }),
  });
  return envelope(0, toBinary(WatchEventsResponseSchema, message));
}

const endStream = envelope(0x02, new TextEncoder().encode("{}"));

/** streamCalls records the api_stream calls and answers api_stream_cancel. */
function streamCalls(): { calls: StreamCall[]; cancelled: number[] } {
  const calls: StreamCall[] = [];
  const cancelled: number[] = [];
  mocks.invoke.mockImplementation((command: string, args: Record<string, unknown>) => {
    if (command === "api_stream_cancel") {
      cancelled.push(args.id as number);
      return Promise.resolve();
    }
    if (command !== "api_stream") {
      return Promise.reject(new Error(`unexpected command ${command}`));
    }
    return new Promise<void>((resolve, reject) => {
      calls.push({ ...(args as Omit<StreamCall, "resolve" | "reject">), resolve, reject });
    });
  });
  return { calls, cancelled };
}

async function waitFor(check: () => boolean): Promise<void> {
  for (let attempt = 0; attempt < 100 && !check(); attempt += 1) {
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
  expect(check()).toBe(true);
}

beforeEach(() => {
  mocks.invoke.mockReset();
});

describe("unary calls", () => {
  it("forward the request bytes and decode the response", async () => {
    const reply = create(GetStatusResponseSchema, { running: true, version: "1.2.3" });
    mocks.invoke.mockResolvedValue({
      status: 200,
      body: toBase64(toBinary(GetStatusResponseSchema, reply)),
    });

    const status = await client.getStatus({});

    expect(status.running).toBe(true);
    expect(status.version).toBe("1.2.3");
    expect(mocks.invoke).toHaveBeenCalledTimes(1);
    const [command, args] = mocks.invoke.mock.calls[0] as [
      string,
      { service: string; method: string; body: string },
    ];
    expect(command).toBe("api_call");
    expect(args.service).toBe("unison.local.v1.UnisonService");
    expect(args.method).toBe("GetStatus");
    fromBinary(GetStatusRequestSchema, fromBase64(args.body));
  });

  it("turn an error of the daemon into a Connect error", async () => {
    const error = JSON.stringify({ code: "not_found", message: "no such device" });
    mocks.invoke.mockResolvedValue({
      status: 404,
      body: toBase64(new TextEncoder().encode(error)),
    });

    await expect(client.getStatus({})).rejects.toMatchObject({ rawMessage: "no such device" });
  });

  it("report a daemon the bridge cannot reach", async () => {
    mocks.invoke.mockRejectedValue("cannot reach the daemon: no such file");

    await expect(client.getStatus({})).rejects.toThrow(/cannot reach the daemon/);
  });

  it("carry large bodies both ways", async () => {
    const text = "ж".repeat(200_000);
    const reply = create(GetStatusResponseSchema, { version: text });
    mocks.invoke.mockResolvedValueOnce({ status: 200, body: "" }).mockResolvedValueOnce({
      status: 200,
      body: toBase64(toBinary(GetStatusResponseSchema, reply)),
    });

    await client.setClip({ text });
    const [, args] = mocks.invoke.mock.calls[0] as [string, { body: string }];
    expect(fromBinary(SetClipRequestSchema, fromBase64(args.body)).text).toBe(text);

    const status = await client.getStatus({});
    expect(status.version).toBe(text);
  });
});

describe("streaming calls", () => {
  it("deliver the events in order, also when a frame spans chunks", async () => {
    const { calls } = streamCalls();
    const received: string[] = [];
    const reading = (async () => {
      for await (const response of client.watchEvents({})) {
        received.push(response.event?.deviceId ?? "");
      }
    })();

    await waitFor(() => calls.length === 1);
    const call = calls[0];
    expect(call.service).toBe("unison.local.v1.UnisonService");
    expect(call.method).toBe("WatchEvents");
    expect(call.id).toBeGreaterThan(0);

    const first = eventFrame(EventKind.PEER_ONLINE, "aa");
    const second = eventFrame(EventKind.PEER_OFFLINE, "bb");
    call.onChunk.onmessage({ chunk: toBase64(first) });
    call.onChunk.onmessage({ chunk: toBase64(second.subarray(0, 3)) });
    call.onChunk.onmessage({ chunk: toBase64(second.subarray(3)) });
    call.onChunk.onmessage({ chunk: toBase64(endStream) });
    call.resolve();

    await reading;
    expect(received).toEqual(["aa", "bb"]);
  });

  it("fail the reader when the bridge fails", async () => {
    const { calls } = streamCalls();
    const reading = (async () => {
      for await (const response of client.watchEvents({})) {
        void response;
      }
    })();

    await waitFor(() => calls.length === 1);
    calls[0].reject("reading the event stream failed: connection reset");

    await expect(reading).rejects.toThrow(/connection reset/);
  });

  it("cancel the stream in the bridge when the reader aborts", async () => {
    const { calls, cancelled } = streamCalls();
    const controller = new AbortController();
    const received: string[] = [];
    const reading = (async () => {
      for await (const response of client.watchEvents({}, { signal: controller.signal })) {
        received.push(response.event?.deviceId ?? "");
      }
    })();

    await waitFor(() => calls.length === 1);
    calls[0].onChunk.onmessage({ chunk: toBase64(eventFrame(EventKind.PEER_ONLINE, "aa")) });
    await waitFor(() => received.length === 1);
    controller.abort();

    await expect(reading).rejects.toThrow();
    expect(cancelled).toEqual([calls[0].id]);
  });

  it("give every stream its own id", async () => {
    const { calls } = streamCalls();
    const controllers = [new AbortController(), new AbortController()];
    const readers = controllers.map((controller) =>
      (async () => {
        for await (const response of client.watchEvents({}, { signal: controller.signal })) {
          void response;
        }
      })().catch(() => {}),
    );

    await waitFor(() => calls.length === 2);
    expect(calls[0].id).not.toBe(calls[1].id);
    for (const controller of controllers) {
      controller.abort();
    }
    await Promise.all(readers);
  });

  it("do not open a stream for an aborted request", async () => {
    const { calls } = streamCalls();
    const controller = new AbortController();
    controller.abort();

    const response = await invokeFetch("http://unison/unison.local.v1.UnisonService/WatchEvents", {
      method: "POST",
      headers: { "content-type": "application/connect+proto" },
      body: new Uint8Array(),
      signal: controller.signal,
    });

    await expect(response.arrayBuffer()).rejects.toThrow();
    expect(calls).toHaveLength(0);
  });
});

describe("paths", () => {
  it("refuse a path without a service and a method", async () => {
    await expect(invokeFetch("http://unison/GetStatus", { method: "POST" })).rejects.toThrow(
      /unexpected local API path/,
    );
    await expect(
      invokeFetch("http://unison/unison.local.v1.UnisonService/", { method: "POST" }),
    ).rejects.toThrow(/unexpected local API path/);
    expect(mocks.invoke).not.toHaveBeenCalled();
  });
});
