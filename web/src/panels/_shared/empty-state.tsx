import type { ReactNode } from "react";

/**
 * Neutral empty-state card. Matches the visual weight of the panels it
 * replaces so the page doesn't visually jump when data loads in.
 */
export function EmptyState({
  title,
  body,
  action
}: {
  title: string;
  body?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="empty-state">
      <h3 className="empty-state-title">{title}</h3>
      {body ? <p className="empty-state-body">{body}</p> : null}
      {action ? <div className="empty-state-action">{action}</div> : null}
    </div>
  );
}
