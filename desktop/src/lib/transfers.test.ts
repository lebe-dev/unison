import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  AcceptTransferResponseSchema,
  CancelTransferResponseSchema,
  EventKind,
  EventSchema,
  ListTransfersResponseSchema,
  RejectTransferResponseSchema,
  SendFilesResponseSchema,
  TransferDirection,
  TransferSchema,
  TransferState,
} from "./api/gen/unison/local/v1/local_pb";
import { client } from "./api/client";
import { transfers } from "./transfers.svelte";

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

vi.mock("./api/client", () => ({
  client: {
    listTransfers: vi.fn(),
    sendFiles: vi.fn(),
    cancelTransfer: vi.fn(),
    acceptTransfer: vi.fn(),
    rejectTransfer: vi.fn(),
  },
}));

vi.mock("./daemon.svelte", () => ({
  daemon: {
    onEvent: harness.onEvent,
    devices: [
      { deviceId: "phone", online: true, revoked: false },
      { deviceId: "laptop", online: false, revoked: false },
      { deviceId: "old", online: true, revoked: true },
    ],
  },
}));

const listTransfers = vi.mocked(client.listTransfers);
const sendFiles = vi.mocked(client.sendFiles);
const cancelTransfer = vi.mocked(client.cancelTransfer);
const acceptTransfer = vi.mocked(client.acceptTransfer);
const rejectTransfer = vi.mocked(client.rejectTransfer);

beforeEach(() => {
  transfers.stop();
  vi.clearAllMocks();
  transfers.transfers = [];
  transfers.error = null;
  transfers.notice = null;
  transfers.clearDraft();
  harness.handlers.length = 0;
});

describe("draft offer", () => {
  it("picks the online devices for new files", () => {
    transfers.offer(["/tmp/a.bin", "/tmp/b.bin"]);

    expect(transfers.picked).toEqual(["/tmp/a.bin", "/tmp/b.bin"]);
    expect(transfers.targets).toEqual(["phone"]);
  });

  it("ignores an empty list", () => {
    transfers.offer(["/tmp/a.bin"]);
    transfers.offer([]);

    expect(transfers.picked).toEqual(["/tmp/a.bin"]);
  });

  it("toggles a target", () => {
    transfers.offer(["/tmp/a.bin"]);
    transfers.toggleTarget("phone");
    expect(transfers.targets).toEqual([]);
    transfers.toggleTarget("phone");
    expect(transfers.targets).toEqual(["phone"]);
  });

  it("sends the draft and clears it", async () => {
    sendFiles.mockResolvedValue(create(SendFilesResponseSchema, { transfers: [] }));
    transfers.offer(["/tmp/a.bin"]);

    await transfers.sendDraft();

    expect(sendFiles).toHaveBeenCalledWith(
      expect.objectContaining({ paths: ["/tmp/a.bin"], deviceIds: ["phone"] }),
    );
    expect(transfers.picked).toEqual([]);
    expect(transfers.targets).toEqual([]);
  });

  it("keeps the draft when the send fails", async () => {
    sendFiles.mockRejectedValue(new Error("offline"));
    transfers.offer(["/tmp/a.bin"]);

    await transfers.sendDraft();

    expect(transfers.picked).toEqual(["/tmp/a.bin"]);
    expect(transfers.notice?.kind).toBe("error");
  });
});

describe("transfer list", () => {
  it("orders the transfers from the newest to the oldest", async () => {
    listTransfers.mockResolvedValue(
      create(ListTransfersResponseSchema, {
        transfers: [
          transfer({ id: "old", createdAtUnixMs: 1n, state: TransferState.DONE }),
          transfer({ id: "new", createdAtUnixMs: 10n, state: TransferState.DONE }),
        ],
      }),
    );

    await transfers.refresh();

    expect(transfers.transfers.map((item) => item.id)).toEqual(["new", "old"]);
    expect(transfers.active).toHaveLength(0);
    expect(transfers.finished).toHaveLength(2);
  });

  it("reports a failure of the daemon", async () => {
    listTransfers.mockRejectedValue(new Error("connection refused"));

    await transfers.refresh();

    expect(transfers.error).toBe("connection refused");
  });

  it("splits the running and the finished transfers", async () => {
    listTransfers.mockResolvedValue(
      create(ListTransfersResponseSchema, {
        transfers: [
          transfer({ id: "running", state: TransferState.TRANSFERRING }),
          transfer({ id: "waiting", state: TransferState.PENDING }),
          transfer({ id: "done", state: TransferState.DONE }),
        ],
      }),
    );

    await transfers.refresh();

    expect(transfers.active.map((item) => item.id).sort()).toEqual(["running", "waiting"]);
    expect(transfers.finished.map((item) => item.id)).toEqual(["done"]);
  });
});

describe("transfer actions", () => {
  it("sends files to the chosen devices", async () => {
    sendFiles.mockResolvedValue(
      create(SendFilesResponseSchema, { transfers: [transfer({ id: "offer" })] }),
    );

    await transfers.send(["/tmp/a.bin"], ["device-1"]);

    expect(sendFiles).toHaveBeenCalledWith(
      expect.objectContaining({ paths: ["/tmp/a.bin"], deviceIds: ["device-1"] }),
    );
    expect(transfers.transfers.map((item) => item.id)).toEqual(["offer"]);
    expect(transfers.notice?.kind).toBe("info");
  });

  it("keeps an error of an empty send", async () => {
    sendFiles.mockRejectedValue(new Error("no files to send"));

    await transfers.send(["/tmp/a.bin"], ["device-1"]);

    expect(transfers.notice).toEqual({ kind: "error", text: "no files to send" });
  });

  it("cancels, accepts and declines a transfer", async () => {
    cancelTransfer.mockResolvedValue(create(CancelTransferResponseSchema, {}));
    acceptTransfer.mockResolvedValue(create(AcceptTransferResponseSchema, {}));
    rejectTransfer.mockResolvedValue(create(RejectTransferResponseSchema, {}));

    await transfers.cancel("t1");
    await transfers.accept("t2");
    await transfers.reject("t3");

    expect(cancelTransfer).toHaveBeenCalledWith(expect.objectContaining({ id: "t1" }));
    expect(acceptTransfer).toHaveBeenCalledWith(expect.objectContaining({ id: "t2" }));
    expect(rejectTransfer).toHaveBeenCalledWith(expect.objectContaining({ id: "t3" }));
  });
});

describe("transfer events", () => {
  it("replaces the record of a running transfer", () => {
    transfers.start();
    transfers.transfers = [transfer({ id: "t1", state: TransferState.OFFERED })];

    harness.handlers[0](
      create(EventSchema, {
        kind: EventKind.TRANSFER,
        transfer: transfer({
          id: "t1",
          state: TransferState.TRANSFERRING,
          bytes: 512n,
          totalBytes: 1024n,
        }),
      }),
    );

    expect(transfers.transfers).toHaveLength(1);
    expect(transfers.transfers[0].state).toBe(TransferState.TRANSFERRING);
    expect(transfers.transfers[0].bytes).toBe(512n);
    transfers.stop();
  });

  it("adds an unknown transfer of an event", () => {
    transfers.start();

    harness.handlers[0](
      create(EventSchema, {
        kind: EventKind.TRANSFER,
        transfer: transfer({ id: "t2", state: TransferState.DONE }),
      }),
    );

    expect(transfers.transfers.map((item) => item.id)).toEqual(["t2"]);
    transfers.stop();
  });
});

function transfer(fields: {
  id: string;
  state?: TransferState;
  bytes?: bigint;
  totalBytes?: bigint;
  createdAtUnixMs?: bigint;
}) {
  return create(TransferSchema, {
    id: fields.id,
    direction: TransferDirection.INCOMING,
    state: fields.state ?? TransferState.TRANSFERRING,
    deviceId: "device-1",
    deviceName: "MacBook",
    bytes: fields.bytes ?? 0n,
    totalBytes: fields.totalBytes ?? 100n,
    createdAtUnixMs: fields.createdAtUnixMs ?? 1n,
  });
}
