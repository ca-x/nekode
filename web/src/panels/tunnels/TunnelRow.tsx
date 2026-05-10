import { useState } from "react";
import type { TunnelRecord } from "../../types";
import { useT } from "../../i18n/use-t";
import type { MessageKey } from "../../i18n/types";
import { CopyButton } from "../_shared/copy-button";

// TunnelRow is the single-item card. Admins see Approve/Reject on
// pending rows; creators see Close on active rows; everyone else sees a
// read-only row. publicUrl only ships to the creator on CreateTunnel, so
// the URL + CopyButton simply stay hidden whenever it's blank.
export function TunnelRow({
  tunnel,
  userIsAdmin,
  currentUserId,
  fmtDate,
  onApprove,
  onReject,
  onClose
}: {
  tunnel: TunnelRecord;
  userIsAdmin: boolean;
  currentUserId: string;
  fmtDate: (unixSeconds: number) => string;
  onApprove: () => void;
  onReject: (reason: string) => void;
  onClose: () => void;
}) {
  const { t } = useT();
  const [rejectOpen, setRejectOpen] = useState(false);
  const [rejectReason, setRejectReason] = useState("");

  const isCreator = tunnel.creatorId === currentUserId;
  const isPending = tunnel.state === "pending_approval";
  const isActive = tunnel.state === "active";

  return (
    <li className={`tunnel-card tunnel-state-${tunnel.state}`}>
      <div className="tunnel-card-heading">
        <div>
          <strong>{tunnel.label || `localhost:${tunnel.localPort}`}</strong>
          <span className={`tunnel-state-badge tunnel-state-${tunnel.state}`}>
            {t(stateLabelKey(tunnel.state))}
          </span>
        </div>
        <span className="detail-muted tabular-nums">{fmtDate(tunnel.createdUnix)}</span>
      </div>
      <dl className="tunnel-meta">
        <div>
          <dt>{t("tunnels.labels.port")}</dt>
          <dd className="tabular-nums">{tunnel.localPort}</dd>
        </div>
        <div>
          <dt>{t("tunnels.labels.accessPolicy")}</dt>
          <dd>{t(accessPolicyLabelKey(tunnel.accessPolicy))}</dd>
        </div>
        <div>
          <dt>{t("tunnels.labels.expires")}</dt>
          <dd className="tabular-nums">{tunnel.expiresUnix > 0 ? fmtDate(tunnel.expiresUnix) : "—"}</dd>
        </div>
      </dl>
      {tunnel.publicUrl ? (
        <div className="tunnel-url-row">
          <a className="tunnel-url" href={tunnel.publicUrl} target="_blank" rel="noreferrer">
            {tunnel.publicUrl}
          </a>
          <CopyButton value={tunnel.publicUrl} ariaLabel={t("common.copy")} />
        </div>
      ) : null}
      {tunnel.closeReason ? (
        <p className="detail-muted">{t("tunnels.labels.closeReason", { reason: tunnel.closeReason })}</p>
      ) : null}
      <div className="tunnel-actions">
        {isPending && userIsAdmin ? (
          <>
            <button type="button" className="primary-button" onClick={onApprove}>
              {t("tunnels.actions.approve")}
            </button>
            <button type="button" className="danger-button" onClick={() => setRejectOpen(true)}>
              {t("tunnels.actions.reject")}
            </button>
          </>
        ) : null}
        {isActive && (isCreator || userIsAdmin) ? (
          <button type="button" className="ghost-button" onClick={onClose}>
            {t("tunnels.actions.close")}
          </button>
        ) : null}
      </div>
      {rejectOpen ? (
        <div className="tunnel-reject-inline">
          <textarea
            rows={2}
            value={rejectReason}
            onChange={(event) => setRejectReason(event.target.value)}
            placeholder={t("tunnels.rejectPlaceholder")}
          />
          <div className="modal-shell-footer-row">
            <button type="button" className="ghost-button" onClick={() => setRejectOpen(false)}>
              {t("common.cancel")}
            </button>
            <button
              type="button"
              className="danger-button"
              onClick={() => {
                onReject(rejectReason);
                setRejectOpen(false);
                setRejectReason("");
              }}
            >
              {t("tunnels.actions.reject")}
            </button>
          </div>
        </div>
      ) : null}
    </li>
  );
}

function stateLabelKey(state: TunnelRecord["state"]): MessageKey {
  switch (state) {
    case "active":
      return "tunnels.state.active";
    case "rejected":
      return "tunnels.state.rejected";
    case "closed":
      return "tunnels.state.closed";
    case "pending_approval":
    default:
      return "tunnels.state.pending";
  }
}

function accessPolicyLabelKey(policy: TunnelRecord["accessPolicy"]): MessageKey {
  switch (policy) {
    case "private":
      return "tunnels.accessPolicy.private";
    case "public":
      return "tunnels.accessPolicy.public";
    case "members":
    default:
      return "tunnels.accessPolicy.members";
  }
}
