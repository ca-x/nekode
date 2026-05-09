import { changeLocale } from "./index";
import { LOCALE_REGISTRY } from "./locales/registry";
import { useLocale, useT } from "./use-t";
import type { Locale } from "./types";

export function LocaleSwitcher({ className }: { className?: string }) {
  const { t } = useT();
  const active = useLocale();

  const onChange = (event: React.ChangeEvent<HTMLSelectElement>) => {
    void changeLocale(event.target.value as Locale);
  };

  return (
    <label className={className ?? "locale-switcher"}>
      <span className="locale-switcher-label">{t("nav.settings")}</span>
      <select aria-label={t("nav.settings")} value={active} onChange={onChange}>
        {LOCALE_REGISTRY.map((entry) => (
          <option key={entry.code} value={entry.code}>
            {entry.nativeLabel}
          </option>
        ))}
      </select>
    </label>
  );
}
