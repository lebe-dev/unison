import { describe, expect, it } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import { cleanError, errorText, isUnavailable, isUnimplemented } from "./errors";

describe("errorText", () => {
  it("drops the package prefix of a daemon message", () => {
    const error = new ConnectError("node: pairing window is closed", Code.FailedPrecondition);
    expect(errorText(error, "fallback")).toBe("pairing window is closed");
  });

  it("reports an unreachable daemon", () => {
    const error = new ConnectError("connection refused", Code.Unavailable);
    expect(errorText(error, "fallback")).toBe("The daemon is not reachable.");
  });

  it("falls back when the error has no message", () => {
    expect(errorText(new ConnectError("", Code.Internal), "fallback")).toBe("fallback");
  });

  it("keeps plain errors", () => {
    expect(errorText(new Error("something broke"), "fallback")).toBe("something broke");
    expect(errorText("unknown", "fallback")).toBe("fallback");
  });
});

describe("isUnimplemented", () => {
  it("matches only the missing calls", () => {
    expect(isUnimplemented(new ConnectError("no", Code.Unimplemented))).toBe(true);
    expect(isUnimplemented(new ConnectError("no", Code.Internal))).toBe(false);
    expect(isUnimplemented(new Error("no"))).toBe(false);
  });
});

describe("isUnavailable", () => {
  it("matches only an unreachable daemon", () => {
    expect(isUnavailable(new ConnectError("no", Code.Unavailable))).toBe(true);
    expect(isUnavailable(new ConnectError("no", Code.Unimplemented))).toBe(false);
  });
});

describe("cleanError", () => {
  it("removes a package prefix and trims the text", () => {
    expect(cleanError(" pairing: the code does not match ")).toBe("the code does not match");
    expect(cleanError("the exchange failed")).toBe("the exchange failed");
  });
});
