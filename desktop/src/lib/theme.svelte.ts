/** Theme the user picked in the app. */
export type ThemeMode = "system" | "light" | "dark";

/** STORAGE_KEY keeps the theme across restarts. */
const STORAGE_KEY = "unison.theme";

/** storedMode returns the theme of the last session. */
function storedMode(): ThemeMode {
  if (typeof localStorage === "undefined") {
    return "system";
  }
  const value = localStorage.getItem(STORAGE_KEY);
  return value === "light" || value === "dark" ? value : "system";
}

/**
 * ThemeStore applies the theme of the app to the document. The system theme
 * follows the operating system while no explicit theme is chosen.
 */
class ThemeStore {
  mode = $state<ThemeMode>(storedMode());

  #query: MediaQueryList | null = null;
  #onChange = () => this.apply();

  /** start follows the system theme and applies the stored one. */
  start(): void {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function" || this.#query) {
      this.apply();
      return;
    }
    this.#query = window.matchMedia("(prefers-color-scheme: dark)");
    this.#query.addEventListener("change", this.#onChange);
    this.apply();
  }

  /** stop releases the system subscription. */
  stop(): void {
    this.#query?.removeEventListener("change", this.#onChange);
    this.#query = null;
  }

  /** set stores and applies the given theme. */
  set(mode: ThemeMode): void {
    this.mode = mode;
    if (typeof localStorage !== "undefined") {
      localStorage.setItem(STORAGE_KEY, mode);
    }
    this.apply();
  }

  /** apply writes the theme attribute of the document. */
  apply(): void {
    if (typeof document === "undefined") {
      return;
    }
    const root = document.documentElement;
    if (this.mode === "system") {
      delete root.dataset.theme;
      return;
    }
    root.dataset.theme = this.mode;
  }
}

/** theme is the theme state of the app. */
export const theme = new ThemeStore();
