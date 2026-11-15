import { beforeEach, describe, expect, it } from "vitest";
import { theme } from "./theme.svelte";

describe("theme", () => {
  beforeEach(() => {
    localStorage.clear();
    theme.mode = "system";
    theme.apply();
  });

  it("follows the system theme without an explicit choice", () => {
    theme.set("system");

    expect(document.documentElement.dataset.theme).toBeUndefined();
    expect(localStorage.getItem("unison.theme")).toBe("system");
  });

  it("applies and stores an explicit theme", () => {
    theme.set("dark");

    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(localStorage.getItem("unison.theme")).toBe("dark");

    theme.set("light");

    expect(document.documentElement.dataset.theme).toBe("light");
  });
});
