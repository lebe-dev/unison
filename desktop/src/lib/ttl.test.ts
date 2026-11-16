import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { SettingsSchema } from "./api/gen/unison/local/v1/local_pb";
import { lifetimeText, maxLifetime, parseLifetime, ttlFromSettings, ttlRequest } from "./ttl";

describe("history lifetimes", () => {
  it("renders a lifetime in the largest whole unit", () => {
    expect(lifetimeText(7n * 86400n)).toBe("7d");
    expect(lifetimeText(86400n)).toBe("1d");
    expect(lifetimeText(90n * 60n)).toBe("90m");
    expect(lifetimeText(45n)).toBe("45s");
    expect(lifetimeText(0n)).toBe("");
  });

  it("parses the units of a lifetime", () => {
    expect(parseLifetime("7d")).toBe(7n * 86400n);
    expect(parseLifetime("24h")).toBe(86400n);
    expect(parseLifetime(" 90M ")).toBe(5400n);
    expect(parseLifetime("45s")).toBe(45n);
    expect(parseLifetime("365d")).toBe(maxLifetime);
  });

  it("refuses a lifetime outside the range", () => {
    expect(parseLifetime("0d")).toBeNull();
    expect(parseLifetime("366d")).toBeNull();
    expect(parseLifetime("week")).toBeNull();
    expect(parseLifetime("")).toBeNull();
    expect(parseLifetime("-1d")).toBeNull();
  });

  it("reads the lifetimes of the settings", () => {
    const settings = create(SettingsSchema, {
      historyTtlTextSeconds: 7n * 86400n,
      historyTtlImageSeconds: 3n * 86400n,
      historyTtlFilesSeconds: 86400n,
    });
    expect(ttlFromSettings(settings)).toEqual({
      text: 7n * 86400n,
      image: 3n * 86400n,
      files: 86400n,
    });
    expect(ttlFromSettings(undefined)).toBeNull();
  });

  it("builds the request of one kind", () => {
    expect(ttlRequest("image", 3600n)).toEqual({ historyTtlImageSeconds: 3600n });
    expect(ttlRequest("files", 3600n)).toEqual({ historyTtlFilesSeconds: 3600n });
    expect(ttlRequest("text", 3600n)).toEqual({ historyTtlTextSeconds: 3600n });
  });
});
