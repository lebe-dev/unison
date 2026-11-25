import type { ThemeMode } from "./theme.svelte";
import type { CheckInterval } from "./updater";

/** Settings of the updater carried by a settings document. */
export interface UpdatesSection {
  enabled?: boolean;
  betaChannel?: boolean;
  interval?: CheckInterval;
}

/** Local settings of the desktop app carried by a settings document. */
export interface DesktopSection {
  theme?: ThemeMode;
  loginItem?: boolean;
  updates?: UpdatesSection;
}

/** The intervals of the background checks. */
export const CHECK_INTERVALS: readonly CheckInterval[] = ["six_hours", "daily", "weekly"];

/** isCheckInterval reports an interval the updater offers. */
export function isCheckInterval(value: unknown): value is CheckInterval {
  return (CHECK_INTERVALS as readonly unknown[]).includes(value);
}

/** The document section of the desktop app, ready for the daemon. */
export function appSectionOf(
  theme: ThemeMode,
  loginItem: boolean,
  updates?: UpdatesSection,
): string {
  const desktop: Record<string, unknown> = { theme, login_item: loginItem };
  if (updates) {
    desktop.updates = updatesDocument(updates);
  }
  return JSON.stringify({ desktop });
}

/** updatesDocument writes the updater settings with the names of the file. */
function updatesDocument(updates: UpdatesSection): Record<string, unknown> {
  const document: Record<string, unknown> = {};
  if (updates.enabled !== undefined) {
    document.enabled = updates.enabled;
  }
  if (updates.betaChannel !== undefined) {
    document.beta_channel = updates.betaChannel;
  }
  if (updates.interval !== undefined) {
    document.interval = updates.interval;
  }
  return document;
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
  const updates = updatesSectionOf(field(desktop, "updates"));
  if (updates) {
    section.updates = updates;
  }
  return section;
}

/** updatesSectionOf reads the updater settings of a document. */
function updatesSectionOf(value: unknown): UpdatesSection | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return undefined;
  }
  const source = value as Record<string, unknown>;
  const section: UpdatesSection = {};
  if (typeof source.enabled === "boolean") {
    section.enabled = source.enabled;
  }
  if (typeof source.beta_channel === "boolean") {
    section.betaChannel = source.beta_channel;
  }
  if (isCheckInterval(source.interval)) {
    section.interval = source.interval;
  }
  return Object.keys(section).length > 0 ? section : undefined;
}

/** field returns the named property of a parsed JSON value. */
function field(value: unknown, key: string): unknown {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return undefined;
  }
  return (value as Record<string, unknown>)[key];
}
