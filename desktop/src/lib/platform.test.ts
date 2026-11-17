import { describe, expect, it } from "vitest";
import { isMac, markPlatform } from "./platform";

describe("platform", () => {
  it("recognizes the user agent of macOS", () => {
    expect(
      isMac("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML)"),
    ).toBe(true);
    expect(isMac("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")).toBe(false);
  });

  it("tags the document with the platform", () => {
    const root = document.createElement("html");
    markPlatform(root);
    expect(["mac", "other"]).toContain(root.dataset.platform);
  });
});
