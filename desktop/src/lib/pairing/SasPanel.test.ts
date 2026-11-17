import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/svelte";
import { create } from "@bufbuild/protobuf";
import { PairingMethod, Platform, Role, SasSchema } from "../api/gen/unison/local/v1/local_pb";
import type { PairingRequest } from "../pairing.svelte";
import SasPanel from "./SasPanel.svelte";

function request(overrides: Partial<PairingRequest> = {}): PairingRequest {
  return {
    deviceID: "aa".repeat(32),
    name: "Pixel 9",
    platform: Platform.ANDROID,
    role: Role.PERSONAL,
    method: PairingMethod.SAS,
    sas: create(SasSchema, { digits: "123456", emoji: ["🦊", "🌵", "🚀", "🍀"] }),
    ...overrides,
  };
}

describe("SasPanel", () => {
  it("shows the device, the grouped digits and four emoji", () => {
    render(SasPanel, { props: { request: request(), onConfirm: vi.fn(), onReject: vi.fn() } });

    expect(screen.getByText("Pixel 9")).toBeTruthy();
    expect(screen.getByText("123 456")).toBeTruthy();
    for (const symbol of ["🦊", "🌵", "🚀", "🍀"]) {
      expect(screen.getByText(symbol)).toBeTruthy();
    }
  });

  it("reports the proposed role", () => {
    render(SasPanel, {
      props: { request: request({ role: Role.FRIENDS }), onConfirm: vi.fn(), onReject: vi.fn() },
    });

    expect(screen.getByText("Friends")).toBeTruthy();
  });

  it("calls the handlers of the decision buttons", async () => {
    const onConfirm = vi.fn();
    const onReject = vi.fn();
    render(SasPanel, { props: { request: request(), onConfirm, onReject } });

    await fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await fireEvent.click(screen.getByRole("button", { name: "Reject" }));

    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(onReject).toHaveBeenCalledTimes(1);
  });

  it("blocks the buttons while the call is in flight", () => {
    render(SasPanel, {
      props: { request: request(), busy: true, onConfirm: vi.fn(), onReject: vi.fn() },
    });

    expect(screen.getByRole("button", { name: "Confirm" }).hasAttribute("disabled")).toBe(true);
    expect(screen.getByRole("button", { name: "Reject" }).hasAttribute("disabled")).toBe(true);
  });

  it("waits for the other device after the code was accepted", () => {
    render(SasPanel, {
      props: { request: request(), confirmed: true, onConfirm: vi.fn(), onReject: vi.fn() },
    });

    expect(screen.getByText(/Waiting for the other device/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Confirm" }).hasAttribute("disabled")).toBe(true);
  });

  it("asks to add the device of a scanned square", async () => {
    const onConfirm = vi.fn();
    render(SasPanel, {
      props: {
        request: request({ method: PairingMethod.QR, sas: null }),
        onConfirm,
        onReject: vi.fn(),
      },
    });

    expect(screen.getByText(/read the square/)).toBeTruthy();
    expect(screen.queryByText("123 456")).toBeNull();
    await fireEvent.click(screen.getByRole("button", { name: "Add device" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("keeps working without a code", () => {
    render(SasPanel, {
      props: { request: request({ sas: null }), onConfirm: vi.fn(), onReject: vi.fn() },
    });

    expect(screen.getByText("Waiting for the code...")).toBeTruthy();
  });
});
