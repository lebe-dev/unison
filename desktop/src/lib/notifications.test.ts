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
import { openFiles, showInFolder } from "./files";
import { history } from "./history.svelte";
import { notifications } from "./notifications.svelte";
import { notify, onNotificationAction } from "./notify";
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

vi.mock("./notify", () => ({
  notify: vi.fn(),
  onNotificationAction: vi.fn(),
}));

vi.mock("./requests.svelte", () => ({
  requests: { decide: vi.fn() },
}));

vi.mock("./history.svelte", () => ({
  history: { apply: vi.fn() },
}));

vi.mock("./files", () => ({
  showInFolder: vi.fn(),
  openFiles: vi.fn(),
}));

const notifyMock = vi.mocked(notify);
const onActionMock = vi.mocked(onNotificationAction);
const decideMock = vi.mocked(requests.decide);
const applyMock = vi.mocked(history.apply);
const showInFolderMock = vi.mocked(showInFolder);
const openFilesMock = vi.mocked(openFiles);

let performed: ((action: { actionId: string; extra: Record<string, unknown> }) => void) | null =
  null;

beforeEach(() => {
  notifications.stop();
  vi.clearAllMocks();
  performed = null;
  notifyMock.mockResolvedValue(true);
  showInFolderMock.mockResolvedValue(undefined);
  openFilesMock.mockResolvedValue(undefined);
  onActionMock.mockImplementation((handler) => {
    performed = handler;
    return Promise.resolve(() => {});
  });
  harness.handlers.length = 0;
});

describe("notifications of daemon events", () => {
  it("announces an item that waits for a download", async () => {
    await notifications.start();
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
    expect(request.actions).toBeUndefined();
  });

  it("shows the same event once", async () => {
    await notifications.start();
    const event = create(EventSchema, {
      kind: EventKind.CLIP_DOWNLOAD,
      name: "MacBook",
      item: create(ClipItemSchema, { id: "item-1", kind: Kind.IMAGE }),
    });

    harness.handlers[0](event);
    harness.handlers[0](event);

    expect(notifyMock).toHaveBeenCalledTimes(1);
  });

  it("offers to apply an item that waits for confirmation", async () => {
    await notifications.start();
    harness.handlers[0](
      create(EventSchema, {
        kind: EventKind.CLIP_APPLY,
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
    expect(request.title).toBe("Clipboard from MacBook");
    expect(request.body).not.toContain("secret");
    expect(request.key).toBe("hold:item-1");
    expect(request.extra).toEqual({ id: "item-1" });
    expect(request.actions?.map((action) => action.title)).toEqual(["Apply"]);
  });

  it("announces a request of Friends with the answer buttons", async () => {
    await notifications.start();
    harness.handlers[0](
      create(EventSchema, {
        kind: EventKind.TRANSFER,
        transfer: transfer(TransferState.PENDING, TransferDirection.INCOMING),
      }),
    );

    expect(notifyMock).toHaveBeenCalledTimes(1);
    const request = notifyMock.mock.calls[0][0];
    expect(request.title).toBe("Files from MacBook");
    expect(request.body).toBe("report.pdf (2.0 KB) waits for your answer.");
    expect(request.key).toBe("request:t1");
    expect(request.extra).toEqual({ id: "t1" });
    expect(request.actions?.map((action) => action.title)).toEqual(["Accept", "Decline"]);
  });

  it("keeps the offer of another device out of the notifications", async () => {
    await notifications.start();
    harness.handlers[0](
      create(EventSchema, {
        kind: EventKind.TRANSFER,
        transfer: transfer(TransferState.PENDING, TransferDirection.OUTGOING),
      }),
    );

    expect(notifyMock).not.toHaveBeenCalled();
  });

  it("announces a received transfer", async () => {
    await notifications.start();

    harness.handlers[0](
      create(EventSchema, {
        kind: EventKind.TRANSFER,
        transfer: transfer(TransferState.DONE, TransferDirection.INCOMING),
      }),
    );

    expect(notifyMock).toHaveBeenCalledWith({
      title: "Files received",
      body: "1 file from MacBook is saved.",
      key: "transfer:t1:done",
    });
  });

  it("offers to show and open the received files", async () => {
    await notifications.start();

    harness.handlers[0](
      create(EventSchema, {
        kind: EventKind.TRANSFER,
        transfer: transfer(TransferState.DONE, TransferDirection.INCOMING, "", [
          "/Users/mac/Downloads/report.pdf",
        ]),
      }),
    );

    const request = notifyMock.mock.calls[0][0];
    expect(request.actions?.map((action) => action.title)).toEqual(["Show in folder", "Open"]);
    expect(request.extra).toEqual({ paths: ["/Users/mac/Downloads/report.pdf"] });
  });

  it("announces a sent transfer", async () => {
    await notifications.start();

    harness.handlers[0](
      create(EventSchema, {
        kind: EventKind.TRANSFER,
        transfer: transfer(TransferState.DONE, TransferDirection.OUTGOING),
      }),
    );

    expect(notifyMock).toHaveBeenCalledWith({
      title: "Files sent",
      body: "1 file is sent to MacBook.",
      key: "transfer:t1:done",
    });
  });

  it("announces a failed transfer", async () => {
    await notifications.start();

    harness.handlers[0](
      create(EventSchema, {
        kind: EventKind.TRANSFER,
        transfer: transfer(TransferState.FAILED, TransferDirection.INCOMING, "the peer is gone"),
      }),
    );

    expect(notifyMock).toHaveBeenCalledWith({
      title: "Transfer failed",
      body: "1 file with MacBook: the peer is gone",
      key: "transfer:t1:failed",
    });
  });
});

describe("answers from the notifications", () => {
  it("accepts and declines a request", async () => {
    await notifications.start();
    expect(performed).not.toBeNull();

    performed?.({ actionId: "accept", extra: { id: "t1" } });
    performed?.({ actionId: "decline", extra: { id: "t2" } });

    expect(decideMock).toHaveBeenNthCalledWith(1, "t1", true);
    expect(decideMock).toHaveBeenNthCalledWith(2, "t2", false);
  });

  it("applies a held item", async () => {
    await notifications.start();

    performed?.({ actionId: "apply", extra: { id: "item-1" } });

    expect(applyMock).toHaveBeenCalledWith("item-1");
  });

  it("shows the received files in the folder", async () => {
    await notifications.start();

    performed?.({ actionId: "show-in-folder", extra: { paths: ["/Users/mac/a.pdf"] } });

    expect(showInFolderMock).toHaveBeenCalledWith(["/Users/mac/a.pdf"]);
  });

  it("opens the received files", async () => {
    await notifications.start();

    performed?.({ actionId: "open", extra: { paths: ["/Users/mac/a.pdf", "/Users/mac/b.pdf"] } });

    expect(openFilesMock).toHaveBeenCalledWith(["/Users/mac/a.pdf", "/Users/mac/b.pdf"]);
  });

  it("ignores an action without a subject", async () => {
    await notifications.start();

    performed?.({ actionId: "apply", extra: {} });
    performed?.({ actionId: "open", extra: {} });

    expect(applyMock).not.toHaveBeenCalled();
    expect(openFilesMock).not.toHaveBeenCalled();
  });
});

function transfer(
  state: TransferState,
  direction: TransferDirection,
  error = "",
  paths: string[] = [],
) {
  return create(TransferSchema, {
    id: "t1",
    direction,
    state,
    deviceName: "MacBook",
    error,
    files: [{ name: "report.pdf", size: 2048n }],
    totalBytes: 2048n,
    paths,
  });
}
