import type { MessageKey } from "./en";
import { detectLocale, translateIn, type Locale, type MessageParams } from "./translate";

/** The locale is chosen once at startup, so a change only happens in tests. */
let current = $state<Locale>(detectLocale());

/**
 * t renders a message in the locale of the app. It reads the reactive locale,
 * so a component that calls it re-renders when the locale changes.
 */
export function t(key: MessageKey, params?: MessageParams): string {
  return translateIn(current, key, params);
}

/** translate is the same lookup for code that does not run in a component. */
export function translate(key: MessageKey, params?: MessageParams): string {
  return translateIn(current, key, params);
}

/** getLocale returns the locale of the app. */
export function getLocale(): Locale {
  return current;
}

/** setLocale switches the locale. The app has no picker; tests use it. */
export function setLocale(locale: Locale): void {
  current = locale;
}
