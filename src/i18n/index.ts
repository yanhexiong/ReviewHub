import { enUS } from "./locales/en-US";
import { zhCN } from "./locales/zh-CN";
import type { Locale, MessageCatalog, TranslationKey } from "./types";

export { supportedLocales, type Locale, type TranslationKey } from "./types";
export { enUS } from "./locales/en-US";
export { zhCN } from "./locales/zh-CN";

const catalogs: Record<Locale, MessageCatalog> = {
  "zh-CN": zhCN,
  "en-US": enUS,
};

export function isLocale(value: string | null | undefined): value is Locale {
  return value === "zh-CN" || value === "en-US";
}

export function translate(
  locale: Locale,
  key: TranslationKey,
  variables?: Record<string, string | number>,
) {
  let value = catalogs[locale][key] ?? catalogs["zh-CN"][key];
  for (const [name, replacement] of Object.entries(variables ?? {})) {
    value = value.replaceAll(`{${name}}`, String(replacement));
  }
  return value;
}
