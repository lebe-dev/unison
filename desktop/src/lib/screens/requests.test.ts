import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen } from "@testing-library/svelte";
import {
  DecideRequestResponseSchema,
  GetStatusResponseSchema,
  ListRequestsResponseSchema,
  ShareRequestSchema,
} from "../api/gen/unison/local/v1/local_pb";
import { client } from "../api/client";
import { daemon } from "../daemon.svelte";
import { requests } from "../requests.svelte";
import Requests from "./Requests.svelte";

vi.mock("../api/client", () => ({
  client: {
    listRequests: vi.fn(),
    decideRequest: vi.fn(),
  },
}));

const listRequests = vi.mocked(client.listRequests);
const decideRequest = vi.mocked(client.decideRequest);

beforeEach(() => {
  vi.clearAllMocks();
  requests.requests = [];
  requests.error = null;
  requests.notice = null;
  requests.answering = [];
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
