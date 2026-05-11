import { useCallback, useEffect, useMemo, useState } from "react";
import type { ApiClient } from "../../api";
import type { TunnelAccessPolicy, TunnelRecord, TunnelState } from "../../types";
import type { MessageKey } from "../../i18n/types";
import { useT, useFormat } from "../../i18n/use-t";
import { TabBar } from "../_shared/tab-bar";
import { EmptyState } from "../_shared/empty-state";
import { AlertPill } from "../_shared/alert-pill";
import { TunnelRow } from "./TunnelRow";
import { CreateTunnelModal, CreatedTunnelModal } from "./CreateTunnelModal";

type TunnelsTab = "active" | "pending" | "closed";

const TAB_DEFS: ReadonlyArray<{ key: TunnelsTab; labelKey: MessageKey; states: TunnelState[] }> = [
  { key: "active", labelKey: "tunnels.tabActive", states: ["active"] },
  { key: "pending", labelKey: "tunnels.tabPending", states: ["pending_approval"] },
  { key: "closed", labelKey: "tunnels.tabClosed", states: ["closed", "rejected"] }
];

/**
 * TunnelsPanel surfaces preview tunnels for a single computer. Admins see
 * approval controls; creators see close + copy URL; everyone else sees a
 * read-only row. Token + publicUrl are only present on the response to
 * the creator (server strips them from list responses), so the copy
 * button is hidden whenever publicUrl is blank.
 */
export function TunnelsPanel({
  api,
  computerId,
  userIsAdmin,
  currentUserId
}: {
  api: ApiClient;
  computerId: string;
  userIsAdmin: boolean;
  currentUserId: string;
}) {
  const { t } = useT();
  const fmt = useFormat();

  const [tab, setTab] = useState<TunnelsTab>("active");
  const [tunnels, setTunnels] = useState<TunnelRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [createdTunnel, setCreatedTunnel] = useState<TunnelRecord | null>(null);

  const refresh = useCallback(async () => {
    if (!computerId) return;
    setLoading(true);
    setError("");
    try {
      const bucket = TAB_DEFS.find((entry) => entry.key === tab)!.states;
      // The REST endpoint supports a single state filter; for the closed
      // tab we fan out to two calls and merge, since closed+rejected both
      // land in that bucket in the UI but are distinct states server-side.
      const responses = await Promise.all(
        bucket.map((state) => api.listTunnels({ computerId, state, limit: 100 }))
      );
      const merged = responses.flatMap((r) => r.items ?? []);
      merged.sort((a, b) => b.createdUnix - a.createdUnix);
      setTunnels(merged);
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to load tunnels");
    } finally {
      setLoading(false);
    }
  }, [api, computerId, tab]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const pendingCount = useMemo(
    () => tunnels.filter((entry) => entry.state === "pending_approval").length,
    [tunnels]
  );

  async function handleCreate(form: { localPort: number; label: string; accessPolicy: TunnelAccessPolicy; ttlSeconds: number }) {
    try {
      const created = await api.createTunnel({
        computerId,
        localPort: form.localPort,
        label: form.label,
        accessPolicy: form.accessPolicy,
        ttlSeconds: form.ttlSeconds
      });
      setCreateOpen(false);
      setCreatedTunnel(created);
      setTab("active");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to create tunnel");
    }
  }

  async function handleApprove(id: string) {
    try {
      await api.approveTunnel(id);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to approve");
    }
  }

  async function handleReject(id: string, reason: string) {
    try {
      await api.rejectTunnel(id, reason);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to reject");
    }
  }

  async function handleClose(id: string) {
    try {
      await api.closeTunnel(id);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to close");
    }
  }

  return (
    <section className="tunnels-panel" aria-label={t("tunnels.panelTitle")}>
      <header className="decisions-header">
        <div>
          <h3>{t("tunnels.panelTitle")}</h3>
          <p className="detail-muted">{t("tunnels.panelHint")}</p>
        </div>
        <button type="button" className="primary-button" onClick={() => setCreateOpen(true)}>
          {t("tunnels.actions.create")}
        </button>
      </header>

      <TabBar
        tabs={TAB_DEFS.map((entry) => ({
          key: entry.key,
          labelKey: entry.labelKey,
          count: entry.key === "pending" ? pendingCount : undefined
        }))}
        active={tab}
        onChange={setTab}
        ariaLabel={t("tunnels.panelTitle")}
      />

      {error ? (
        <div className="inline-error-row">
          <AlertPill tone="danger" label={error} />
          <button className="ghost-button" type="button" onClick={() => void refresh()}>
            {t("common.retry")}
          </button>
        </div>
      ) : null}
      {loading ? <p className="detail-muted">{t("common.loading")}</p> : null}

      {!loading && tunnels.length === 0 ? (
        <EmptyState title={t(emptyTitleKey(tab))} body={t(emptyBodyKey(tab))} />
      ) : null}

      <ul className="tunnels-list" role="list">
        {tunnels.map((tunnel) => (
          <TunnelRow
            key={tunnel.id}
            tunnel={tunnel}
            userIsAdmin={userIsAdmin}
            currentUserId={currentUserId}
            fmtDate={(unix) => fmt.dateTime(unix * 1000)}
            onApprove={() => handleApprove(tunnel.id)}
            onReject={(reason) => handleReject(tunnel.id, reason)}
            onClose={() => handleClose(tunnel.id)}
          />
        ))}
      </ul>

      <CreateTunnelModal open={createOpen} onClose={() => setCreateOpen(false)} onSubmit={handleCreate} />
      <CreatedTunnelModal
        tunnel={createdTunnel}
        onClose={() => setCreatedTunnel(null)}
      />
    </section>
  );
}

function emptyTitleKey(tab: TunnelsTab): MessageKey {
  if (tab === "pending") return "tunnels.emptyPendingTitle";
  if (tab === "closed") return "tunnels.emptyClosedTitle";
  return "tunnels.emptyActiveTitle";
}

function emptyBodyKey(tab: TunnelsTab): MessageKey {
  if (tab === "pending") return "tunnels.emptyPendingBody";
  if (tab === "closed") return "tunnels.emptyClosedBody";
  return "tunnels.emptyActiveBody";
}
