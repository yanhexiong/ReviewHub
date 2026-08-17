"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import { translate, type Locale, type TranslationKey } from "@/i18n";
import {
  LOCALE_STORAGE_KEY,
  parseStoredLocale,
  parseStoredTheme,
  readStoredPreferences,
  THEME_STORAGE_KEY,
  writeStoredPreferences,
} from "./preference-storage";
import type { Theme } from "./types";

type PreferencesContextValue = {
  locale: Locale;
  theme: Theme;
  setLocale: (locale: Locale) => void;
  setTheme: (theme: Theme) => void;
  toggleTheme: () => void;
  t: (
    key: TranslationKey,
    variables?: Record<string, string | number>,
  ) => string;
};

const PreferencesContext = createContext<PreferencesContextValue | null>(null);

export function PreferencesProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const [locale, setLocale] = useState<Locale>("zh-CN");
  const [theme, setTheme] = useState<Theme>("light");

  useEffect(() => {
    const stored = readStoredPreferences(window.localStorage);
    const storedLocale = parseStoredLocale(stored.locale);
    const storedTheme = parseStoredTheme(stored.theme);
    if (storedLocale) setLocale(storedLocale);
    if (storedTheme) setTheme(storedTheme);

    const syncPreferences = (event: StorageEvent) => {
      if (event.key === LOCALE_STORAGE_KEY) {
        const nextLocale = parseStoredLocale(event.newValue);
        if (nextLocale) setLocale(nextLocale);
      }
      if (event.key === THEME_STORAGE_KEY) {
        const nextTheme = parseStoredTheme(event.newValue);
        if (nextTheme) setTheme(nextTheme);
      }
    };
    window.addEventListener("storage", syncPreferences);
    return () => window.removeEventListener("storage", syncPreferences);
  }, []);

  useEffect(() => {
    const root = document.documentElement;
    root.lang = locale;
    root.dataset.locale = locale;
    root.dataset.theme = theme;
    writeStoredPreferences(window.localStorage, { locale, theme });
  }, [locale, theme]);

  const toggleTheme = useCallback(() => {
    setTheme((current) => (current === "dark" ? "light" : "dark"));
  }, []);

  const t = useCallback(
    (key: TranslationKey, variables?: Record<string, string | number>) =>
      translate(locale, key, variables),
    [locale],
  );

  const value = useMemo(
    () => ({ locale, theme, setLocale, setTheme, toggleTheme, t }),
    [locale, theme, toggleTheme, t],
  );

  return (
    <PreferencesContext.Provider value={value}>
      {children}
    </PreferencesContext.Provider>
  );
}

export function usePreferences() {
  const context = useContext(PreferencesContext);
  if (!context)
    throw new Error("usePreferences must be used inside PreferencesProvider");
  return context;
}
