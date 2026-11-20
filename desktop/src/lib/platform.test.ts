import { describe, expect, it } from "vitest";
import { translateIn } from "./i18n";
import { deviceNameKey, isLinux, isMac, isWindows, markPlatform, systemName } from "./platform";

const MAC = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML)";
const WINDOWS = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36";
const LINUX = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36";
const ANDROID = "Mozilla/5.0 (Linux; Android 15; Pixel 8) AppleWebKit/537.36";

describe("platform", () => {
  it("recognizes the user agent of macOS", () => {
    expect(isMac(MAC)).toBe(true);
    expect(isMac(WINDOWS)).toBe(false);
  });

  it("recognizes the user agent of Windows", () => {
    expect(isWindows(WINDOWS)).toBe(true);
    expect(isWindows(MAC)).toBe(false);
    expect(isWindows(LINUX)).toBe(false);
  });

  it("recognizes the user agent of Linux", () => {
    expect(isLinux(LINUX)).toBe(true);
    expect(isLinux(MAC)).toBe(false);
    expect(isLinux(WINDOWS)).toBe(false);
    expect(isLinux(ANDROID)).toBe(false);
  });

  it("names this device after the platform", () => {
    expect(translateIn("en", deviceNameKey(MAC))).toBe("This Mac");
    expect(translateIn("ru", deviceNameKey(MAC))).toBe("Этот Mac");
    expect(translateIn("en", deviceNameKey(WINDOWS))).toBe("This PC");
    expect(translateIn("ru", deviceNameKey(WINDOWS))).toBe("Этот компьютер");
    expect(translateIn("en", deviceNameKey(LINUX))).toBe("This computer");
    expect(translateIn("ru", deviceNameKey(LINUX))).toBe("Этот компьютер");
    expect(translateIn("en", deviceNameKey(ANDROID))).toBe("This device");
    expect(translateIn("ru", deviceNameKey(ANDROID))).toBe("Это устройство");
  });

  it("names the system for the log header", () => {
    expect(systemName(MAC)).toBe("macOS");
    expect(systemName(WINDOWS)).toBe("Windows");
    expect(systemName(LINUX)).toBe("Linux");
    expect(systemName(ANDROID)).toBe("Unknown");
  });

  it("tags the document with the platform", () => {
    const root = document.createElement("html");
    markPlatform(root);
    expect(["mac", "other"]).toContain(root.dataset.platform);
  });
});
