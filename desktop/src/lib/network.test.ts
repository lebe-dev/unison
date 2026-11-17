import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  DeviceSchema,
  EventKind,
  EventSchema,
  FailureReason,
} from "./api/gen/unison/local/v1/local_pb";
import { pairingFailure, quietWindowIssue, unreachableDeviceIssue } from "./network";

describe("pairingFailure", () => {
  it("explains an unreachable device in plain words and keeps the error as details", () => {
    const result = pairingFailure(
      create(EventSchema, {
        kind: EventKind.PAIRING_FAILED,
        failureReason: FailureReason.UNREACHABLE,
        error: "pairing: device is not reachable over the network: 192.168.1.2:4000: timeout",
      }),
    );

    expect(result.paired).toBe(false);
    expect(result.message).toBe("Could not reach the other device.");
    expect(result.hint).toMatch(/same network/);
    expect(result.details).toContain("192.168.1.2:4000: timeout");
  });

  it("keeps the daemon message of another failure", () => {
    const result = pairingFailure(
      create(EventSchema, { kind: EventKind.PAIRING_FAILED, error: "pairing: pairing rejected" }),
    );

    expect(result.message).toBe("pairing rejected");
    expect(result.details).toBeUndefined();
  });
});

describe("unreachableDeviceIssue", () => {
  it("is absent for a device that answers", () => {
    expect(unreachableDeviceIssue(create(DeviceSchema, { name: "Pixel", online: true }))).toBeNull();
    expect(unreachableDeviceIssue(create(DeviceSchema, { name: "Pixel" }))).toBeNull();
  });

  it("names the device and lists the addresses in the details", () => {
    const issue = unreachableDeviceIssue(
      create(DeviceSchema, {
        name: "Pixel",
        unreachable: true,
        unreachableSinceUnixMs: 1_700_000_000_000n,
        unreachableDetail: "192.168.1.2:4000: timeout\n100.64.0.2:4000: timeout",
      }),
    );

    expect(issue?.summary).toBe("Pixel does not answer.");
    expect(issue?.hint).toMatch(/turned off/);
    expect(issue?.details).toContain("192.168.1.2:4000: timeout");
    expect(issue?.details).toContain("100.64.0.2:4000: timeout");
    expect(issue?.details).toMatch(/^Not answering since /);
  });
});

describe("quietWindowIssue", () => {
  it("waits before it warns", () => {
    expect(quietWindowIssue(10_000, "0.0.0.0:5000")).toBeNull();
  });

  it("warns about a quiet network with the address in the details", () => {
    const issue = quietWindowIssue(45_000, "0.0.0.0:5000");

    expect(issue?.summary).toBe("No devices found on this network yet.");
    expect(issue?.details).toContain("45 seconds");
    expect(issue?.details).toContain("0.0.0.0:5000");
  });
});
