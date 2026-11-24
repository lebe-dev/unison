import type { ThemeMode } from "./theme.svelte";

/** Local settings of the desktop app carried by a settings document. */
export interface DesktopSection {
  theme?: ThemeMode;
  loginItem?: boolean;
}

/** The document section of the desktop app, ready for the daemon. */
export function appSectionOf(theme: ThemeMode, loginItem: boolean): string {
  return JSON.stringify({ desktop: { theme, login_item: loginItem } });
}

/** desktopSectionOf returns the desktop settings of a settings document. A
 * document without a readable section yields no settings, which keeps an
 * import from another platform and a hand edited file usable. */
export function desktopSectionOf(text: string): DesktopSection {
  let document: unknown;
  try {
    document = JSON.parse(text);
  } catch {
    return {};
  }
  const app = field(document, "app");
  const desktop = field(app, "desktop");
  if (desktop === undefined) {
    return {};
  }

  const section: DesktopSection = {};
  const theme = field(desktop, "theme");
  if (theme === "system" || theme === "light" || theme === "dark") {
    section.theme = theme;
  }
  const loginItem = field(desktop, "login_item");
  if (typeof loginItem === "boolean") {
    section.loginItem = loginItem;
  }
  return section;
}

/** field returns the named property of a parsed JSON value. */
function field(value: unknown, key: string): unknown {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return undefined;
  }
  return (value as Record<string, unknown>)[key];
}
