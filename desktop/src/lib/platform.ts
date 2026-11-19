import type { MessageKey } from "./i18n";

/** isMac reports whether the app runs on macOS, where the window has no title bar
 * and the traffic lights sit over the sidebar. */
export function isMac(userAgent: string = navigator.userAgent): boolean {
  return /Mac OS X|Macintosh/.test(userAgent);
}

/** isWindows reports whether the app runs on Windows. */
export function isWindows(userAgent: string = navigator.userAgent): boolean {
  return /Windows/.test(userAgent);
}

/** deviceNameKey returns the message key of the name of this device on its
 * platform. */
export function deviceNameKey(userAgent: string = navigator.userAgent): MessageKey {
  if (isMac(userAgent)) {
    return "common.thisMac";
  }
  if (isWindows(userAgent)) {
    return "common.thisPC";
  }
  return "common.thisDevice";
}

/** systemName returns the name of the system for the header of the log export. */
export function systemName(userAgent: string = navigator.userAgent): string {
  if (isMac(userAgent)) {
    return "macOS";
  }
  if (isWindows(userAgent)) {
    return "Windows";
  }
  return "Unknown";
}

/** markPlatform tags the document so the styles can follow the platform. */
export function markPlatform(root: HTMLElement = document.documentElement): void {
  root.dataset.platform = isMac() ? "mac" : "other";
}
