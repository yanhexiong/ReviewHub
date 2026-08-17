import type { zhCN } from "./locales/zh-CN";

export const supportedLocales = ["zh-CN", "en-US"] as const;
export type Locale = (typeof supportedLocales)[number];

export type TranslationKey = keyof typeof zhCN;
export type MessageCatalog = Record<TranslationKey, string>;
