import { en, type Message, type MessageKey } from "./en";
import { ru } from "./ru";

/** Language of the user interface. */
export type Locale = "en" | "ru";

/** Values of the placeholders of a message. */
export type MessageParams = Record<string, string | number>;

const catalogs: Record<Locale, { [K in MessageKey]: Message }> = { en, ru };

/**
 * detectLocale picks the language of the app from the language tags of the
 * system. Only the primary language tag counts: Russian becomes Russian,
 * anything else English. The first tag is the one the user prefers.
 */
export function detectLocale(languages?: readonly string[]): Locale {
  const tags = languages ?? systemLanguages();
  const primary = tags[0]?.trim().toLowerCase().split(/[-_]/)[0] ?? "";
  return primary === "ru" ? "ru" : "en";
}

/** systemLanguages returns the language tags of the browser. */
function systemLanguages(): readonly string[] {
  if (typeof navigator === "undefined") {
    return [];
  }
  if (navigator.languages && navigator.languages.length > 0) {
    return navigator.languages;
  }
  return navigator.language ? [navigator.language] : [];
}

/**
 * translateIn renders one message of a catalog. A plural message follows the
 * plural rules of the locale, so Russian gets its one, few and many forms.
 */
export function translateIn(locale: Locale, key: MessageKey, params: MessageParams = {}): string {
  const catalog = catalogs[locale] ?? catalogs.en;
  const message = catalog[key] ?? en[key];
  const template =
    typeof message === "string" ? message : selectPlural(locale, countOf(params), message);
  return interpolate(template, params);
}

/** selectPlural picks the form of a plural message for a count. */
function selectPlural(locale: Locale, count: number, message: Exclude<Message, string>): string {
  const category = new Intl.PluralRules(locale).select(count);
  return message[category] ?? message.other;
}

/** countOf reads the count of a plural message, zero when it is absent. */
function countOf(params: MessageParams): number {
  const value = Number(params.count ?? 0);
  return Number.isFinite(value) ? value : 0;
}

/** interpolate fills the {name} placeholders of a message. */
function interpolate(template: string, params: MessageParams): string {
  return template.replace(/\{(\w+)\}/g, (match, name: string) => {
    const value = params[name];
    return value === undefined ? match : String(value);
  });
}
