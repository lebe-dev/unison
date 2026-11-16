import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  ClipItemSchema,
  EventKind,
  EventSchema,
  Kind,
  TransferDirection,
  TransferSchema,
  TransferState,
} from "./api/gen/unison/local/v1/local_pb";
import { notifications } from "./notifications.svelte";
import { notify } from "./notify";

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

vi.mock("./notify", () => ({
  notify: vi.fn(),
}));

const notifyMock = vi.mocked(notify);

beforeEach(() => {
  notifications.stop();
  vi.clearAllMocks();
  notifyMock.mockResolvedValue(true);
  harness.handlers.length = 0;
});

describe("notifications of daemon events", () => {
  it("announces an item that waits for a download", () => {
    notifications.start();
    harness.handlers[0](
      create(EventSchema, {
        kind: EventKind.CLIP_DOWNLOAD,
        name: "MacBook",
        item: create(ClipItemSchema, {
          id: "item-1",
          kind: Kind.TEXT,
          representations: [
            {
              mimeType: "text/plain",
              size: 6n,
              inlineBytes: new TextEncoder().encode("secret"),
            },
          ],
        }),
      }),
    );

    expect(notifyMock).toHaveBeenCalledTimes(1);
    const request = notifyMock.mock.calls[0][0];
    expect(request.title).toBe("Item available for download");
    expect(request.body).toBe("Text from MacBook waits in the history.");
    expect(request.body).not.toContain("secret");
    notifications.stop();
  });

  it("shows the same event once", () => {
    notifications.start();
    const event = create(EventSchema, {
      kind: EventKind.CLIP_DOWNLOAD,
      name: "MacBook",
      item: create(ClipItemSchema, { id: "item-1", kind: Kind.IMAGE }),
    });

    harness.handlers[0](event);
    harness.handlers[0](event);

    expect(notifyMock).toHaveBeenCalledTimes(1);
    notifications.stop();
  });

  it("announces a received transfer", () => {
    notifications.start();

    harness.handlers[0](
      create(EventSchema, {
        kind: EventKind.TRANSFER,
        transfer: transfer(TransferState.DONE, TransferDirection.INCOMING),
      }),
    );

    expect(notifyMock).toHaveBeenCalledWith({
      title: "Files received",
      body: "1 file from MacBook is saved.",
    });
    notifications.stop();
  });

  it("announces a sent transfer", () => {
    notifications.start();

    harness.handlers[0](
      create(EventSchema, {
        kind: EventKind.TRANSFER,
        transfer: transfer(TransferState.DONE, TransferDirection.OUTGOING),
      }),
    );

    expect(notifyMock).toHaveBeenCalledWith({
      title: "Files sent",
      body: "1 file is sent to MacBook.",
    });
    notifications.stop();
  });

  it("announces a failed transfer", () => {
    notifications.start();

    harness.handlers[0](
      create(EventSchema, {
        kind: EventKind.TRANSFER,
        transfer: transfer(TransferState.FAILED, TransferDirection.INCOMING, "the peer is gone"),
      }),
    );

    expect(notifyMock).toHaveBeenCalledWith({
      title: "Transfer failed",
      body: "1 file with MacBook: the peer is gone",
    });
    notifications.stop();
  });
});

function transfer(state: TransferState, direction: TransferDirection, error = "") {
  return create(TransferSchema, {
    id: "t1",
    direction,
    state,
    deviceName: "MacBook",
    error,
    files: [{ name: "report.pdf", size: 2048n }],
    totalBytes: 2048n,
  });
}
