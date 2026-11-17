/** isMac reports whether the app runs on macOS, where the window has no title bar
 * and the traffic lights sit over the sidebar. */
export function isMac(userAgent: string = navigator.userAgent): boolean {
  return /Mac OS X|Macintosh/.test(userAgent);
}

/** markPlatform tags the document so the styles can follow the platform. */
export function markPlatform(root: HTMLElement = document.documentElement): void {
  root.dataset.platform = isMac() ? "mac" : "other";
}
