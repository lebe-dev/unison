import { afterEach, describe, expect, it } from "vitest";
import { en, type Message, type MessageKey } from "./en";
import { ru } from "./ru";
import { detectLocale, translateIn } from "./translate";
import { getLocale, setLocale, t } from "./state.svelte";

afterEach(() => {
  setLocale("en");
});

describe("locale detection", () => {
  it("picks Russian from a Russian primary tag", () => {
    expect(detectLocale(["ru-RU"])).toBe("ru");
    expect(detectLocale(["ru"])).toBe("ru");
    expect(detectLocale(["RU_ru"])).toBe("ru");
  });

  it("uses English for every other language and for an empty list", () => {
    expect(detectLocale(["en-US"])).toBe("en");
    expect(detectLocale(["de-DE"])).toBe("en");
    expect(detectLocale(["uk-UA"])).toBe("en");
    expect(detectLocale([])).toBe("en");
    expect(detectLocale()).toBe("en");
  });

  it("looks at the primary tag of the first language only", () => {
    expect(detectLocale(["ru-RU", "en-US"])).toBe("ru");
    expect(detectLocale(["en-US", "ru-RU"])).toBe("en");
  });
});

describe("fallback", () => {
  it("keeps English when the locale is switched and back", () => {
    expect(t("nav.devices")).toBe("Devices");
    setLocale("ru");
    expect(getLocale()).toBe("ru");
    expect(t("nav.devices")).toBe("Устройства");
    setLocale("en");
    expect(t("nav.devices")).toBe("Devices");
  });
});

describe("interpolation", () => {
  it("fills the placeholders of a message", () => {
    expect(translateIn("en", "devices.renamed", { name: "Pixel" })).toBe(
      "The device is now called Pixel.",
    );
    expect(translateIn("ru", "devices.renamed", { name: "Pixel" })).toBe(
      "Теперь устройство называется Pixel.",
    );
  });

  it("keeps a placeholder without a value", () => {
    expect(translateIn("en", "history.imageFrom")).toBe("Image from {name}");
  });
});

describe("plural forms", () => {
  it("uses one and other in English", () => {
    expect(translateIn("en", "history.itemsCount", { count: 1 })).toBe("1 item");
    expect(translateIn("en", "history.itemsCount", { count: 2 })).toBe("2 items");
    expect(translateIn("en", "history.itemsCount", { count: 11 })).toBe("11 items");
  });

  it("uses one, few and many in Russian", () => {
    expect(translateIn("ru", "history.itemsCount", { count: 1 })).toBe("1 элемент");
    expect(translateIn("ru", "history.itemsCount", { count: 2 })).toBe("2 элемента");
    expect(translateIn("ru", "history.itemsCount", { count: 5 })).toBe("5 элементов");
    expect(translateIn("ru", "history.itemsCount", { count: 21 })).toBe("21 элемент");
  });
});

describe("catalog parity", () => {
  it("has every English key in Russian", () => {
    expect(Object.keys(ru).sort()).toEqual(Object.keys(en).sort());
  });

  it("has a value for every key", () => {
    for (const key of Object.keys(en) as MessageKey[]) {
      expect(translateIn("en", key, { count: 1, name: "Pixel" })).not.toBe("");
      expect(translateIn("ru", key, { count: 1, name: "Pixel" })).not.toBe("");
    }
  });

  it("keeps the placeholders of every message", () => {
    for (const key of Object.keys(en) as MessageKey[]) {
      expect(placeholders(ru[key]), key).toEqual(placeholders(en[key]));
    }
  });
});

/** placeholders collects the {name} placeholders of every form of a message. */
function placeholders(message: Message): string[] {
  const found = new Set<string>();
  for (const form of forms(message)) {
    for (const match of form.matchAll(/\{(\w+)\}/g)) {
      found.add(match[1]);
    }
  }
  return [...found].sort();
}

/** forms returns the strings of a message. */
function forms(message: Message): string[] {
  if (typeof message === "string") {
    return [message];
  }
  return [message.zero, message.one, message.two, message.few, message.many, message.other].filter(
    (form): form is string => typeof form === "string",
  );
}

/** platformMessages name a platform on purpose: the name of this device and
 * the hints that exist only on macOS. */
const platformMessages = new Set<MessageKey>([
  "common.thisMac",
  "app.localNetworkHint",
  "settings.loginHint.requiresApproval",
]);

describe("platform names", () => {
  it("keeps the name of another system out of the messages", () => {
    for (const key of Object.keys(en) as MessageKey[]) {
      if (platformMessages.has(key)) {
        continue;
      }
      for (const form of [...forms(en[key]), ...forms(ru[key])]) {
        expect(form, key).not.toMatch(/Mac|macOS|System Settings|Login Items|Finder/);
      }
    }
  });
});
