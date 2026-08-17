import { isLocale, type Locale } from "@/i18n";
import type { StoredPreferences, Theme } from "./types";

export const LOCALE_STORAGE_KEY = "review-hub-locale";
export const THEME_STORAGE_KEY = "review-hub-theme";

type BrowserStorage = Pick<Storage, "getItem" | "setItem">;

export function isTheme(value: string | null | undefined): value is Theme {
  return value === "light" || value === "dark";
}

export function readStoredPreferences(
  storage: BrowserStorage,
): StoredPreferences {
  return {
    locale: storage.getItem(LOCALE_STORAGE_KEY),
    theme: storage.getItem(THEME_STORAGE_KEY),
  };
}

export function writeStoredPreferences(
  storage: BrowserStorage,
  preferences: { locale: Locale; theme: Theme },
) {
  storage.setItem(LOCALE_STORAGE_KEY, preferences.locale);
  storage.setItem(THEME_STORAGE_KEY, preferences.theme);
}

export function parseStoredLocale(value: string | null): Locale | null {
  return isLocale(value) ? value : null;
}

export function parseStoredTheme(value: string | null): Theme | null {
  return isTheme(value) ? value : null;
}
