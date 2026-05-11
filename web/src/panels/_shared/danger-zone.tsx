import { useState } from "react";
import type { ReactNode } from "react";
import { useT } from "../../i18n/use-t";

/**
 * Danger-zone banner for destructive actions (Delete Computer / Delete Agent).
 * Two-step confirmation: first click asks "are you sure", second confirms.
 */
export function DangerZone({
  title,
  description,
  onConfirm,
  disabledReason,
  busy
}: {
  title: string;
  description?: ReactNode;
  onConfirm: () => void;
  disabledReason?: string;
  busy?: boolean;
}) {
  const { t } = useT();
  const [confirming, setConfirming] = useState(false);

  return (
    <section className="danger-zone" aria-label={title}>
      <header className="danger-zone-header">
        <h3 className="danger-zone-title">{title}</h3>
        {description ? <p className="danger-zone-description">{description}</p> : null}
        {disabledReason ? (
          <p className="danger-zone-helper" role="note">
            {disabledReason}
          </p>
        ) : null}
      </header>
      <div className="danger-zone-action">
        {confirming ? (
          <div className="danger-zone-confirm">
            <button className="danger-button" type="button" onClick={onConfirm} disabled={busy}>
              {busy ? t("common.loading") : t("common.confirm")}
            </button>
            <button className="ghost-button" type="button" onClick={() => setConfirming(false)}>
              {t("common.cancel")}
            </button>
          </div>
        ) : (
          <button className="danger-button" type="button" onClick={() => setConfirming(true)} disabled={Boolean(disabledReason)}>
            {title}
          </button>
        )}
      </div>
    </section>
  );
}
