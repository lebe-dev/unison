import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen } from "@testing-library/svelte";
import {
  ConfirmMediatedPairResponseSchema,
  DecideRequestResponseSchema,
  GetStatusResponseSchema,
  ListMediatedPairsResponseSchema,
  ListRequestsResponseSchema,
  MediatedDeviceSchema,
  MediatedPairDirection,
  MediatedPairSchema,
  MediatedPairState,
  Platform,
  RejectMediatedPairResponseSchema,
  Role,
  SasSchema,
  ShareRequestSchema,
} from "../api/gen/unison/local/v1/local_pb";
import { client } from "../api/client";
import { daemon } from "../daemon.svelte";
import { mediated } from "../mediated.svelte";
import { requests } from "../requests.svelte";
import Requests from "./Requests.svelte";

vi.mock("../api/client", () => ({
  client: {
    listRequests: vi.fn(),
    decideRequest: vi.fn(),
    listMediatedPairs: vi.fn(),
    confirmMediatedPair: vi.fn(),
    rejectMediatedPair: vi.fn(),
    cancelMediatedPair: vi.fn(),
  },
}));

const listRequests = vi.mocked(client.listRequests);
const decideRequest = vi.mocked(client.decideRequest);
const listMediatedPairs = vi.mocked(client.listMediatedPairs);
const confirmMediatedPair = vi.mocked(client.confirmMediatedPair);
const rejectMediatedPair = vi.mocked(client.rejectMediatedPair);

beforeEach(() => {
  vi.clearAllMocks();
  requests.requests = [];
  requests.error = null;
  requests.notice = null;
  requests.answering = [];
  mediated.pairs = [];
  mediated.error = null;
  mediated.notice = null;
  mediated.answering = [];
  listMediatedPairs.mockResolvedValue(create(ListMediatedPairsResponseSchema, {}));
  confirmMediatedPair.mockResolvedValue(create(ConfirmMediatedPairResponseSchema, {}));
  rejectMediatedPair.mockResolvedValue(create(RejectMediatedPairResponseSchema, {}));
  daemon.status = create(GetStatusResponseSchema, {
    running: true,
    deviceId: "self",
    deviceName: "This Mac",
  });
  daemon.error = null;
});

describe("requests screen", () => {
  it("shows the offers that wait for an answer", async () => {
    listRequests.mockResolvedValue(
      create(ListRequestsResponseSchema, { requests: [offer("t1", "MacBook")] }),
    );

    render(Requests);

    expect(await screen.findByText("MacBook")).toBeTruthy();
    expect(screen.getByText("report.pdf")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Accept" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Decline" })).toBeTruthy();
  });

  it("accepts an offer of the list", async () => {
    listRequests.mockResolvedValue(
      create(ListRequestsResponseSchema, { requests: [offer("t1", "MacBook")] }),
    );
    decideRequest.mockResolvedValue(create(DecideRequestResponseSchema, {}));

    render(Requests);
    await fireEvent.click(await screen.findByRole("button", { name: "Accept" }));

    expect(decideRequest).toHaveBeenCalledWith({ id: "t1", accept: true });
    expect(await screen.findByText("The files are on their way.")).toBeTruthy();
    expect(screen.getByText("No file waits for your answer.")).toBeTruthy();
  });

  it("declines an offer of the list", async () => {
    listRequests.mockResolvedValue(
      create(ListRequestsResponseSchema, { requests: [offer("t1", "MacBook")] }),
    );
    decideRequest.mockResolvedValue(create(DecideRequestResponseSchema, {}));

    render(Requests);
    await fireEvent.click(await screen.findByRole("button", { name: "Decline" }));

    expect(decideRequest).toHaveBeenCalledWith({ id: "t1", accept: false });
  });

  it("shows an empty list", async () => {
    listRequests.mockResolvedValue(create(ListRequestsResponseSchema, {}));

    render(Requests);

    expect(await screen.findByText("No file waits for your answer.")).toBeTruthy();
  });
});

describe("incoming introductions", () => {
  it("shows the mediator, the new device and the code of the offer", async () => {
    listRequests.mockResolvedValue(create(ListRequestsResponseSchema, {}));
    listMediatedPairs.mockResolvedValue(
      create(ListMediatedPairsResponseSchema, { pairs: [introduction()] }),
    );

    render(Requests);

    expect(await screen.findByText("Introduction from MacBook")).toBeTruthy();
    expect(screen.getByText("Phone")).toBeTruthy();
    expect(screen.getByText(/0123 4567 89AB/)).toBeTruthy();
    expect(screen.getByText("Personal")).toBeTruthy();
    expect(screen.getByText("123 456")).toBeTruthy();
  });

  it("tells that a confirmation replaces the keys of a paired device", async () => {
    listMediatedPairs.mockResolvedValue(
      create(ListMediatedPairsResponseSchema, { pairs: [introduction({ alreadyPaired: true })] }),
    );

    render(Requests);

    expect(
      await screen.findByText(
        "This device is already paired and offline; confirming replaces its connection keys.",
      ),
    ).toBeTruthy();
  });

  it("turns the friends search on by default and passes the choice on", async () => {
    listMediatedPairs.mockResolvedValue(
      create(ListMediatedPairsResponseSchema, {
        pairs: [introduction({ role: Role.FRIENDS })],
      }),
    );

    render(Requests);
    const toggle = await screen.findByRole("checkbox", {
      name: "Turn on friend discovery for 15 minutes after confirming",
    });
    expect((toggle as HTMLInputElement).checked).toBe(true);

    await fireEvent.click(toggle);
    await fireEvent.click(screen.getByRole("button", { name: "Accept" }));

    expect(confirmMediatedPair).toHaveBeenCalledWith({
      sessionId: "s1",
      enableFriendsDiscovery: false,
    });
  });

  it("accepts an offer with the friends search on", async () => {
    listMediatedPairs.mockResolvedValue(
      create(ListMediatedPairsResponseSchema, {
        pairs: [introduction({ role: Role.FRIENDS })],
      }),
    );

    render(Requests);
    await fireEvent.click(await screen.findByRole("button", { name: "Accept" }));

    expect(confirmMediatedPair).toHaveBeenCalledWith({
      sessionId: "s1",
      enableFriendsDiscovery: true,
    });
    expect(await screen.findByText("Confirmed. Waiting for the other device.")).toBeTruthy();
  });

  it("declines an offer", async () => {
    listMediatedPairs.mockResolvedValue(
      create(ListMediatedPairsResponseSchema, { pairs: [introduction()] }),
    );

    render(Requests);
    await fireEvent.click(await screen.findByRole("button", { name: "Decline" }));

    expect(rejectMediatedPair).toHaveBeenCalledWith({ sessionId: "s1" });
    expect(await screen.findByText("The introduction is declined.")).toBeTruthy();
  });
});

/** introduction builds an offer another device makes to this one. */
function introduction(fields: { role?: Role; alreadyPaired?: boolean } = {}) {
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
    expiresAtUnixMs: 1_700_000_600_000n,
    ...fields,
  });
}

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
