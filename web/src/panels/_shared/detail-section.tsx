import type { ReactNode } from "react";

/**
 * Named horizontal rule with optional trailing slot for section-level actions.
 *
 * Reused across Computer/Agent detail pages for INFO / RUNTIME CONFIG /
 * ENVIRONMENT VARIABLES / ACTIONS blocks so every section shares the same
 * spacing, title type scale, and focus-order.
 */
export function DetailSection({
  title,
  hint,
  trailing,
  children
}: {
  title: string;
  hint?: string;
  trailing?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="detail-section">
      <header className="detail-section-header">
        <div>
          <h3 className="detail-section-title">{title}</h3>
          {hint ? <p className="detail-section-hint">{hint}</p> : null}
        </div>
        {trailing ? <div className="detail-section-actions">{trailing}</div> : null}
      </header>
      <div className="detail-section-body">{children}</div>
    </section>
  );
}
