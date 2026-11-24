import { describe, expect, it } from "vitest";
import { appSectionOf, desktopSectionOf } from "./settingsFile";

describe("appSectionOf", () => {
  it("describes the theme and the login item", () => {
    expect(JSON.parse(appSectionOf("dark", true))).toEqual({
      desktop: { theme: "dark", login_item: true },
    });
  });
});

describe("desktopSectionOf", () => {
  it("reads the section of a document", () => {
    const text = JSON.stringify({
      schema: "unison-settings",
      format_version: 1,
      core: { strip_tracking: false },
      app: { desktop: { theme: "light", login_item: false } },
    });
    expect(desktopSectionOf(text)).toEqual({ theme: "light", loginItem: false });
  });

  it("ignores the sections of other platforms", () => {
    const text = JSON.stringify({ app: { android: { operating_mode: "always" } } });
    expect(desktopSectionOf(text)).toEqual({});
  });

  it("keeps the known keys of a section with unknown ones", () => {
    const text = JSON.stringify({ app: { desktop: { future: true, theme: "system" } } });
    expect(desktopSectionOf(text)).toEqual({ theme: "system" });
  });

  it("drops values of the wrong type", () => {
    const text = JSON.stringify({ app: { desktop: { theme: "sepia", login_item: "yes" } } });
    expect(desktopSectionOf(text)).toEqual({});
  });

  it("accepts text that is not a document", () => {
    expect(desktopSectionOf("not json")).toEqual({});
    expect(desktopSectionOf("[]")).toEqual({});
    expect(desktopSectionOf("null")).toEqual({});
  });
});
