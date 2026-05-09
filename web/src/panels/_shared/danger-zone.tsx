import type { ReactNode } from "react";

/**
 * Danger-zone banner for destructive actions (Delete Computer / Delete Agent).
 * Always pairs the destructive button with an explanation + disabled reason
 * when applicable so users never have to guess why a button is grey.
 */
export function DangerZone({
  title,
  description,
  action,
  disabledReason
}: {
  title: string;
  description?: ReactNode;
  action: ReactNode;
  disabledReason?: string;
}) {
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
      <div className="danger-zone-action">{action}</div>
    </section>
  );
}
