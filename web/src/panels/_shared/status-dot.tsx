import type { ReactNode } from "react";

export type StatusTone = "online" | "offline" | "running" | "idle" | "warning" | "error" | "neutral";

const TONE_TO_CLASS: Record<StatusTone, string> = {
  online: "status-dot-online",
  offline: "status-dot-offline",
  running: "status-dot-running",
  idle: "status-dot-idle",
  warning: "status-dot-warning",
  error: "status-dot-error",
  neutral: "status-dot-neutral"
};

/**
 * Small color-coded dot with an adjacent label. Accessible by announcing the
 * label text; the dot itself is decorative. Callers always pair the dot with
 * a label so colorblind users still get the status.
 */
export function StatusDot({ tone, label, trailing }: { tone: StatusTone; label: ReactNode; trailing?: ReactNode }) {
  return (
    <span className="status-dot-wrapper">
      <span className={`status-dot ${TONE_TO_CLASS[tone]}`} aria-hidden="true" />
      <span className="status-dot-label">{label}</span>
      {trailing}
    </span>
  );
}

/** Convenience: infer the tone from a last-heartbeat timestamp in seconds. */
export function heartbeatTone(lastHeartbeatUnix: number | undefined, nowMs: number = Date.now()): StatusTone {
  if (!lastHeartbeatUnix || lastHeartbeatUnix <= 0) return "offline";
  const ageSeconds = nowMs / 1000 - lastHeartbeatUnix;
  if (ageSeconds < 60) return "online";
  if (ageSeconds < 300) return "warning";
  return "offline";
}
