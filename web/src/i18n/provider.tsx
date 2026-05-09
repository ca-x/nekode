import { useEffect, type ReactNode } from "react";
import { I18nextProvider } from "react-i18next";

import { i18n } from "./index";
import { useLocale } from "./use-t";
import { LOCALE_REGISTRY } from "./locales/registry";

/**
 * Keeps the <html lang="..."> and dir attributes in sync with the active
 * locale so screen readers, browser spell-check, and CSS :lang()
 * selectors behave correctly.
 */
function LocaleDocumentBinding({ children }: { children: ReactNode }) {
  const locale = useLocale();
  useEffect(() => {
    const descriptor = LOCALE_REGISTRY.find((entry) => entry.code === locale) ?? LOCALE_REGISTRY[0];
    document.documentElement.lang = descriptor.intlTag;
    document.documentElement.dir = descriptor.dir;
  }, [locale]);
  return <>{children}</>;
}

export function I18nProvider({ children }: { children: ReactNode }) {
  return (
    <I18nextProvider i18n={i18n}>
      <LocaleDocumentBinding>{children}</LocaleDocumentBinding>
    </I18nextProvider>
  );
}
