import type { ReactNode } from "react";
import { AlertTriangle, Info } from "lucide-react";

export type AlertTone = "info" | "warning" | "danger";

const TONE_CLASS: Record<AlertTone, string> = {
  info: "alert-pill-info",
  warning: "alert-pill-warning",
  danger: "alert-pill-danger"
};

/**
 * Compact inline alert used for the sidebar footer and detail-screen banners.
 * Pairs a semantic color with an icon + text so the meaning isn't color-only.
 */
export function AlertPill({
  tone,
  label,
  action,
  onClick,
  ariaLabel
}: {
  tone: AlertTone;
  label: ReactNode;
  action?: ReactNode;
  onClick?: () => void;
  ariaLabel?: string;
}) {
  const Icon = tone === "info" ? Info : AlertTriangle;
  const body = (
    <>
      <Icon size={14} aria-hidden="true" />
      <span className="alert-pill-label">{label}</span>
      {action ? <span className="alert-pill-action">{action}</span> : null}
    </>
  );
  if (onClick) {
    return (
      <button
        type="button"
        className={`alert-pill ${TONE_CLASS[tone]}`}
        aria-label={ariaLabel ?? (typeof label === "string" ? label : undefined)}
        onClick={onClick}
      >
        {body}
      </button>
    );
  }
  return (
    <div className={`alert-pill ${TONE_CLASS[tone]}`} role="status" aria-label={ariaLabel}>
      {body}
    </div>
  );
}
