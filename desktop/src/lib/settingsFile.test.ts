import { describe, expect, it } from "vitest";
import { appSectionOf, desktopSectionOf, isCheckInterval } from "./settingsFile";

describe("appSectionOf", () => {
  it("describes the theme and the login item", () => {
    expect(JSON.parse(appSectionOf("dark", true))).toEqual({
      desktop: { theme: "dark", login_item: true },
    });
  });

  it("describes the updater settings with the names of the file", () => {
    expect(
      JSON.parse(
        appSectionOf("dark", true, {
          enabled: true,
          betaChannel: false,
          interval: "weekly",
        }),
      ),
    ).toEqual({
      desktop: {
        theme: "dark",
        login_item: true,
        updates: { enabled: true, beta_channel: false, interval: "weekly" },
      },
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

  it("reads the updater settings of a document", () => {
    const text = JSON.stringify({
      app: {
        desktop: {
          updates: { enabled: true, beta_channel: true, interval: "six_hours" },
        },
      },
    });
    expect(desktopSectionOf(text)).toEqual({
      updates: { enabled: true, betaChannel: true, interval: "six_hours" },
    });
  });

  it("round-trips the updater settings", () => {
    const section: unknown = JSON.parse(
      appSectionOf("system", false, {
        enabled: true,
        betaChannel: false,
        interval: "daily",
      }),
    );
    const text = JSON.stringify({ app: section });
    expect(desktopSectionOf(text)).toEqual({
      theme: "system",
      loginItem: false,
      updates: { enabled: true, betaChannel: false, interval: "daily" },
    });
  });

  it("keeps a document without updater settings usable", () => {
    const text = JSON.stringify({ app: { desktop: { theme: "dark" } } });
    expect(desktopSectionOf(text)).toEqual({ theme: "dark" });
  });

  it("drops an interval the updater does not offer", () => {
    const text = JSON.stringify({ app: { desktop: { updates: { interval: "hourly" } } } });
    expect(desktopSectionOf(text)).toEqual({});
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

describe("isCheckInterval", () => {
  it("accepts the offered intervals only", () => {
    expect(isCheckInterval("six_hours")).toBe(true);
    expect(isCheckInterval("daily")).toBe(true);
    expect(isCheckInterval("weekly")).toBe(true);
    expect(isCheckInterval("hourly")).toBe(false);
    expect(isCheckInterval(24)).toBe(false);
  });
});
