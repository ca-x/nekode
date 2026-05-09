import { useMemo } from "react";
import { useTranslation } from "react-i18next";

import type { Locale, MessageKey } from "./types";
import { DEFAULT_LOCALE, LOCALE_REGISTRY } from "./locales/registry";

type TranslateOptions = Record<string, unknown>;

/**
 * Typed wrapper around react-i18next's useTranslation.
 *
 * Components call useT() instead of importing i18next directly so the
 * module boundary stays crisp: if we ever swap the i18n engine, the
 * surface area to update is this file plus index.ts.
 */
export function useT() {
  const { t: rawT, i18n } = useTranslation();
  const locale = (i18n.resolvedLanguage ?? i18n.language ?? DEFAULT_LOCALE) as Locale;

  function t(key: MessageKey, options?: TranslateOptions): string {
    return rawT(key, options) as string;
  }

  return { t, locale };
}

export function useLocale(): Locale {
  const { i18n } = useTranslation();
  return (i18n.resolvedLanguage ?? i18n.language ?? DEFAULT_LOCALE) as Locale;
}

/**
 * Intl-backed formatters bound to the active locale. Use these for
 * dates, numbers, and relative times instead of hand-rolled strings or
 * i18next formatters so behaviour matches platform conventions.
 */
export function useFormat() {
  const locale = useLocale();
  const descriptor = useMemo(
    () => LOCALE_REGISTRY.find((entry) => entry.code === locale) ?? LOCALE_REGISTRY[0],
    [locale]
  );
  const tag = descriptor.intlTag;

  return useMemo(
    () => ({
      locale,
      dir: descriptor.dir,
      date(value: Date | number, options?: Intl.DateTimeFormatOptions): string {
        return new Intl.DateTimeFormat(tag, options).format(value);
      },
      time(value: Date | number): string {
        return new Intl.DateTimeFormat(tag, { timeStyle: "short" }).format(value);
      },
      dateTime(value: Date | number): string {
        return new Intl.DateTimeFormat(tag, { dateStyle: "medium", timeStyle: "short" }).format(value);
      },
      number(value: number, options?: Intl.NumberFormatOptions): string {
        return new Intl.NumberFormat(tag, options).format(value);
      },
      relative(value: number, unit: Intl.RelativeTimeFormatUnit): string {
        return new Intl.RelativeTimeFormat(tag, { numeric: "auto" }).format(value, unit);
      }
    }),
    [descriptor, locale, tag]
  );
}
