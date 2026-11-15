import { describe, expect, it } from "vitest";
import { Path, Platform, Role } from "./api/gen/unison/local/v1/local_pb";
import { formatTime, pathLabel, platformLabel, roleLabel, shortDeviceID } from "./format";

describe("roleLabel", () => {
  it("names the roles", () => {
    expect(roleLabel(Role.PERSONAL)).toBe("Personal");
    expect(roleLabel(Role.FRIENDS)).toBe("Friends");
    expect(roleLabel(Role.UNSPECIFIED)).toBe("Unknown role");
  });
});

describe("platformLabel", () => {
  it("names the systems", () => {
    expect(platformLabel(Platform.MACOS)).toBe("macOS");
    expect(platformLabel(Platform.WINDOWS)).toBe("Windows");
    expect(platformLabel(Platform.ANDROID)).toBe("Android");
    expect(platformLabel(Platform.UNSPECIFIED)).toBe("Unknown system");
  });
});

describe("pathLabel", () => {
  it("names the connection paths", () => {
    expect(pathLabel(Path.LAN)).toBe("LAN");
    expect(pathLabel(Path.RELAY)).toBe("Relay");
    expect(pathLabel(Path.UNSPECIFIED)).toBe("");
  });
});

describe("shortDeviceID", () => {
  it("groups the first twelve characters", () => {
    expect(shortDeviceID("0123456789abcdef")).toBe("0123 4567 89AB");
    expect(shortDeviceID("abc")).toBe("ABC");
    expect(shortDeviceID("")).toBe("");
  });
});

describe("formatTime", () => {
  it("keeps an absent timestamp empty", () => {
    expect(formatTime(0)).toBe("");
    expect(formatTime(0n)).toBe("");
  });
});
