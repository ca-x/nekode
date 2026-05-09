import i18next, { type i18n } from "i18next";
import { initReactI18next } from "react-i18next";
import LanguageDetector from "i18next-browser-languagedetector";

import type { Locale, LocaleCatalog, MessageKey } from "./types";
import { DEFAULT_LOCALE, LOCALE_REGISTRY } from "./locales/registry";

import en from "./locales/en.json";
import zhCN from "./locales/zh-CN.json";

const CATALOGS: Record<Locale, LocaleCatalog> = {
  en: en as LocaleCatalog,
  "zh-CN": zhCN as LocaleCatalog
};

const STORAGE_KEY = "nekode.locale";

const i18n: i18n = i18next.createInstance();

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    supportedLngs: LOCALE_REGISTRY.map((entry) => entry.code),
    fallbackLng: DEFAULT_LOCALE,
    defaultNS: "translation",
    ns: ["translation"],
    resources: Object.fromEntries(
      (Object.entries(CATALOGS) as Array<[Locale, LocaleCatalog]>).map(([code, catalog]) => [
        code,
        { translation: catalog }
      ])
    ),
    interpolation: {
      // React already escapes interpolated values.
      escapeValue: false
    },
    returnNull: false,
    detection: {
      order: ["localStorage", "navigator", "htmlTag"],
      caches: ["localStorage"],
      lookupLocalStorage: STORAGE_KEY
    },
    react: {
      // Wait for the initial resources to be available before first render
      // so we never flash catalog keys to the user.
      useSuspense: false
    }
  });

export function currentLocale(): Locale {
  return (i18n.resolvedLanguage ?? i18n.language ?? DEFAULT_LOCALE) as Locale;
}

export function changeLocale(locale: Locale): Promise<unknown> {
  return i18n.changeLanguage(locale);
}

/**
 * Imperative translator for code paths that cannot use hooks (utility
 * functions, axios interceptors, etc.). Prefer useT() in components.
 */
export function t(key: MessageKey, options?: Record<string, unknown>): string {
  return i18n.t(key, options) as string;
}

export { i18n, LOCALE_REGISTRY, DEFAULT_LOCALE };
export type { Locale, MessageKey, LocaleCatalog };
