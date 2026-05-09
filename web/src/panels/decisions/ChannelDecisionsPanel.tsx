import { useCallback, useEffect, useMemo, useState } from "react";
import type { ApiClient } from "../../api";
import type { ChannelDecision, ChannelDecisionVote, DecisionStatus, DecisionVote } from "../../types";
import type { MessageKey } from "../../i18n/types";
import { useT, useFormat } from "../../i18n/use-t";
import { TabBar } from "../_shared/tab-bar";
import { EmptyState } from "../_shared/empty-state";
import { AlertPill } from "../_shared/alert-pill";
import { ModalShell } from "../_shared/modal-shell";

type DecisionTab = "proposed" | "ratified" | "closed";

const TAB_DEFS: ReadonlyArray<{ key: DecisionTab; labelKey: MessageKey }> = [
  { key: "proposed", labelKey: "decisions.tabProposed" },
  { key: "ratified", labelKey: "decisions.tabRatified" },
  { key: "closed", labelKey: "decisions.tabClosed" }
];

const STATUSES_BY_TAB: Record<DecisionTab, DecisionStatus[]> = {
  proposed: ["proposed"],
  ratified: ["ratified"],
  closed: ["rejected", "retired"]
};

/**
 * ChannelDecisionsPanel is the user-facing home for governance on a
 * channel: propose, vote, ratify, retire. It owns its own data fetch so
 * the host component (channel settings drawer) can mount it without
 * plumbing the full decision lifecycle through props.
 */
export function ChannelDecisionsPanel({
  api,
  target,
  userIsAdmin,
  currentUserId
}: {
  api: ApiClient;
  target: string;
  userIsAdmin: boolean;
  currentUserId: string;
}) {
  const { t } = useT();
  const fmt = useFormat();

  const [tab, setTab] = useState<DecisionTab>("proposed");
  const [decisions, setDecisions] = useState<ChannelDecision[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [proposing, setProposing] = useState(false);
  const [retiringId, setRetiringId] = useState<string>("");
  const [proposeOpen, setProposeOpen] = useState(false);
  const [votesById, setVotesById] = useState<Record<string, ChannelDecisionVote[]>>({});

  const refresh = useCallback(async () => {
    if (!target) return;
    setLoading(true);
    setError("");
    try {
      const statusList = STATUSES_BY_TAB[tab];
      const result = await api.listChannelDecisions(target, { status: statusList, limit: 100 });
      setDecisions(result.items ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to load decisions");
    } finally {
      setLoading(false);
    }
  }, [api, tab, target]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const proposalCount = useMemo(
    () => decisions.filter((entry) => entry.status === "proposed").length,
    [decisions]
  );

  async function handlePropose(form: { title: string; body: string }) {
    if (!form.title.trim() || !form.body.trim()) return;
    setProposing(true);
    try {
      await api.proposeChannelDecision(target, { title: form.title.trim(), body: form.body.trim() });
      setProposeOpen(false);
      // Switch to the "proposed" tab so the new row is in view immediately.
      setTab("proposed");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to propose decision");
    } finally {
      setProposing(false);
    }
  }

  async function handleVote(decisionID: string, vote: DecisionVote, reason?: string) {
    try {
      const result = await api.voteChannelDecision(decisionID, { decision: vote, reason });
      setDecisions((current) =>
        current.map((entry) => (entry.id === decisionID ? result.decision : entry))
      );
      // If auto-ratification flipped the decision off the proposed tab,
      // refresh so the UI reflects the new bucket.
      if (result.decision.status !== "proposed") {
        await refresh();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to vote");
    }
  }

  async function handleForceRatify(decisionID: string) {
    try {
      const updated = await api.ratifyChannelDecision(decisionID, { force: true });
      setDecisions((current) =>
        current.map((entry) => (entry.id === decisionID ? updated : entry))
      );
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to ratify");
    }
  }

  async function handleRetire(decisionID: string, reason: string) {
    try {
      await api.retireChannelDecision(decisionID, { reason });
      setRetiringId("");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to retire");
    }
  }

  async function ensureVotes(decisionID: string) {
    if (votesById[decisionID]) return votesById[decisionID];
    const result = await api.listDecisionVotes(decisionID);
    setVotesById((current) => ({ ...current, [decisionID]: result.items ?? [] }));
    return result.items ?? [];
  }

  return (
    <section className="decisions-panel" aria-label={t("decisions.panelTitle")}>
      <header className="decisions-header">
        <div>
          <h3>{t("decisions.panelTitle")}</h3>
          <p className="detail-muted">{t("decisions.quorumHint")}</p>
        </div>
        <button type="button" className="primary-button" onClick={() => setProposeOpen(true)}>
          {t("decisions.actions.propose")}
        </button>
      </header>

      <TabBar
        tabs={TAB_DEFS.map((entry) => ({
          key: entry.key,
          labelKey: entry.labelKey,
          count: entry.key === "proposed" ? proposalCount : undefined
        }))}
        active={tab}
        onChange={setTab}
        ariaLabel={t("decisions.panelTitle")}
      />

      {error ? <AlertPill tone="danger" label={error} /> : null}
      {loading ? <p className="detail-muted">{t("common.loading")}</p> : null}

      {!loading && decisions.length === 0 ? (
        <EmptyState
          title={t(emptyTitleKey(tab))}
          body={t(emptyBodyKey(tab))}
        />
      ) : null}

      <ul className="decisions-list" role="list">
        {decisions.map((decision) => (
          <DecisionCard
            key={decision.id}
            decision={decision}
            currentUserId={currentUserId}
            userIsAdmin={userIsAdmin}
            fmtDate={(unix) => fmt.dateTime(unix * 1000)}
            onVote={(vote, reason) => handleVote(decision.id, vote, reason)}
            onForceRatify={() => handleForceRatify(decision.id)}
            onRetireStart={() => setRetiringId(decision.id)}
            onExpandVotes={() => ensureVotes(decision.id)}
            votes={votesById[decision.id]}
          />
        ))}
      </ul>

      <ProposeModal
        open={proposeOpen}
        busy={proposing}
        onClose={() => setProposeOpen(false)}
        onSubmit={handlePropose}
      />

      <RetireModal
        open={retiringId !== ""}
        onClose={() => setRetiringId("")}
        onSubmit={(reason) => handleRetire(retiringId, reason)}
      />
    </section>
  );
}

function emptyTitleKey(tab: DecisionTab): MessageKey {
  if (tab === "proposed") return "decisions.tabProposed";
  if (tab === "ratified") return "decisions.tabRatified";
  return "decisions.tabClosed";
}

function emptyBodyKey(tab: DecisionTab): MessageKey {
  if (tab === "proposed") return "decisions.emptyProposed";
  if (tab === "ratified") return "decisions.emptyRatified";
  return "decisions.emptyClosed";
}

// --- inline components --------------------------------------------------

function DecisionCard({
  decision,
  currentUserId,
  userIsAdmin,
  fmtDate,
  onVote,
  onForceRatify,
  onRetireStart,
  onExpandVotes,
  votes
}: {
  decision: ChannelDecision;
  currentUserId: string;
  userIsAdmin: boolean;
  fmtDate: (unixSeconds: number) => string;
  onVote: (vote: DecisionVote, reason?: string) => void | Promise<void>;
  onForceRatify: () => void | Promise<void>;
  onRetireStart: () => void;
  onExpandVotes: () => Promise<ChannelDecisionVote[]>;
  votes?: readonly ChannelDecisionVote[];
}) {
  const { t } = useT();
  const [expanded, setExpanded] = useState(false);

  const isProposed = decision.status === "proposed";
  const quorumReached = decision.approveCount >= 2 && decision.rejectCount === 0;

  const myVote = useMemo(
    () => votes?.find((entry) => entry.voterId === currentUserId)?.decision ?? null,
    [currentUserId, votes]
  );

  function toggle() {
    const next = !expanded;
    setExpanded(next);
    if (next) void onExpandVotes();
  }

  return (
    <li className={`decision-card decision-status-${decision.status}`}>
      <button type="button" className="decision-card-header" onClick={toggle} aria-expanded={expanded}>
        <div className="decision-card-heading">
          <strong>{decision.title}</strong>
          <span className={`decision-status-badge decision-status-${decision.status}`}>
            {t(decisionStatusLabelKey(decision.status))}
          </span>
        </div>
        <span className="detail-muted tabular-nums">{fmtDate(decision.createdUnix)}</span>
      </button>

      <div className="decision-card-body">
        <p>{decision.body}</p>
        {decision.retiredUnix > 0 ? (
          <AlertPill
            tone="warning"
            label={t("decisions.retiredBanner", {
              reason: decision.retireReason ? ` · ${decision.retireReason}` : ""
            })}
          />
        ) : null}

        <div className="decision-vote-row">
          <span className="tabular-nums" aria-label="approvals">
            ✓ {decision.approveCount}
          </span>
          <span className="tabular-nums" aria-label="rejections">
            ✗ {decision.rejectCount}
          </span>
          <span className="tabular-nums" aria-label="abstentions">
            ◦ {decision.abstainCount}
          </span>
        </div>

        {isProposed ? (
          <div className="decision-actions">
            <button
              type="button"
              className={myVote === "approve" ? "primary-button" : "ghost-button"}
              onClick={() => onVote("approve")}
            >
              {t("decisions.actions.approve")}
            </button>
            <button
              type="button"
              className={myVote === "reject" ? "danger-button" : "ghost-button"}
              onClick={() => onVote("reject")}
            >
              {t("decisions.actions.reject")}
            </button>
            <button
              type="button"
              className={myVote === "abstain" ? "ghost-button is-active" : "ghost-button"}
              onClick={() => onVote("abstain")}
            >
              {t("decisions.actions.abstain")}
            </button>
            {userIsAdmin && !quorumReached ? (
              <button type="button" className="ghost-button" onClick={onForceRatify}>
                {t("decisions.actions.forceRatify")}
              </button>
            ) : null}
            <button type="button" className="ghost-button" onClick={onRetireStart}>
              {t("decisions.actions.retire")}
            </button>
          </div>
        ) : null}

        {expanded && votes ? (
          <ul className="decision-votes-list" role="list">
            {votes.map((entry) => (
              <li key={entry.id}>
                <span>{entry.voterId}</span>
                <span>{t(decisionVoteLabelKey(entry.decision))}</span>
                <span className="detail-muted">{fmtDate(entry.votedUnix)}</span>
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    </li>
  );
}

function ProposeModal({
  open,
  busy,
  onClose,
  onSubmit
}: {
  open: boolean;
  busy: boolean;
  onClose: () => void;
  onSubmit: (form: { title: string; body: string }) => void;
}) {
  const { t } = useT();
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");

  useEffect(() => {
    if (!open) {
      setTitle("");
      setBody("");
    }
  }, [open]);

  return (
    <ModalShell
      open={open}
      title={t("decisions.proposeModal.title")}
      onClose={onClose}
      footer={
        <div className="modal-shell-footer-row">
          <button className="ghost-button" type="button" onClick={onClose}>
            {t("common.cancel")}
          </button>
          <button
            className="primary-button"
            type="button"
            onClick={() => onSubmit({ title, body })}
            disabled={busy || !title.trim() || !body.trim()}
          >
            {t("decisions.proposeModal.submit")}
          </button>
        </div>
      }
    >
      <label className="form-field">
        <span>{t("decisions.proposeModal.titleLabel")}</span>
        <input autoFocus value={title} onChange={(event) => setTitle(event.target.value)} disabled={busy} />
      </label>
      <label className="form-field">
        <span>{t("decisions.proposeModal.bodyLabel")}</span>
        <textarea rows={5} value={body} onChange={(event) => setBody(event.target.value)} disabled={busy} />
      </label>
    </ModalShell>
  );
}

function RetireModal({
  open,
  onClose,
  onSubmit
}: {
  open: boolean;
  onClose: () => void;
  onSubmit: (reason: string) => void;
}) {
  const { t } = useT();
  const [reason, setReason] = useState("");

  useEffect(() => {
    if (!open) setReason("");
  }, [open]);

  return (
    <ModalShell
      open={open}
      title={t("decisions.retireModal.title")}
      onClose={onClose}
      footer={
        <div className="modal-shell-footer-row">
          <button className="ghost-button" type="button" onClick={onClose}>
            {t("common.cancel")}
          </button>
          <button className="danger-button" type="button" onClick={() => onSubmit(reason)}>
            {t("decisions.retireModal.confirm")}
          </button>
        </div>
      }
    >
      <label className="form-field">
        <span>{t("decisions.retireModal.reasonLabel")}</span>
        <textarea rows={3} value={reason} onChange={(event) => setReason(event.target.value)} />
      </label>
    </ModalShell>
  );
}

function decisionStatusLabelKey(status: DecisionStatus): MessageKey {
  switch (status) {
    case "ratified":
      return "decisions.status.ratified";
    case "rejected":
      return "decisions.status.rejected";
    case "retired":
      return "decisions.status.retired";
    case "proposed":
    default:
      return "decisions.status.proposed";
  }
}

function decisionVoteLabelKey(vote: DecisionVote): MessageKey {
  switch (vote) {
    case "reject":
      return "decisions.actions.reject";
    case "abstain":
      return "decisions.actions.abstain";
    case "approve":
    default:
      return "decisions.actions.approve";
  }
}
