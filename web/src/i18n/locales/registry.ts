import type { Locale } from "../types";

export type LocaleDescriptor = {
  code: Locale;
  /** Display label in English (for operator-facing settings). */
  label: string;
  /** Display label in the locale itself — shown in the switcher. */
  nativeLabel: string;
  /** Writing direction. nekode today only supports ltr locales. */
  dir: "ltr" | "rtl";
  /** Intl BCP-47 tag (may differ from the catalog key for some locales). */
  intlTag: string;
};

export const LOCALE_REGISTRY: readonly LocaleDescriptor[] = [
  {
    code: "en",
    label: "English",
    nativeLabel: "English",
    dir: "ltr",
    intlTag: "en"
  },
  {
    code: "zh-CN",
    label: "Chinese (Simplified)",
    nativeLabel: "简体中文",
    dir: "ltr",
    intlTag: "zh-CN"
  }
];

export const DEFAULT_LOCALE: Locale = "en";
