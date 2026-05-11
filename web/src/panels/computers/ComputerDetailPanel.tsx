import { useMemo, useState } from "react";
import type { ApiClient } from "../../api";
import type { DaemonInventoryComputer, RuntimePreset } from "../../types";
import { useT } from "../../i18n/use-t";
import type { MessageKey } from "../../i18n/types";
import { DetailShell } from "../_shared/detail-shell";
import { DetailSection } from "../_shared/detail-section";
import { KeyValueList, type KeyValue } from "../_shared/kv-list";
import { EmptyState } from "../_shared/empty-state";
import { CopyButton } from "../_shared/copy-button";
import { StatusDot, heartbeatTone } from "../_shared/status-dot";
import { AlertPill } from "../_shared/alert-pill";
import { DangerZone } from "../_shared/danger-zone";
import { detectedRuntimeKinds, isComputerOnline } from "./computer-utils";
import { AgentRunsPanel } from "../agents/AgentRunsPanel";
import { TunnelsPanel } from "../tunnels/TunnelsPanel";

type ComputerDetailTab = "overview" | "agents" | "runs" | "tunnels" | "settings";

const TAB_DEFS: ReadonlyArray<{ key: ComputerDetailTab; labelKey: MessageKey }> = [
  { key: "overview", labelKey: "computer.tabs.overview" },
  { key: "agents", labelKey: "computer.tabs.agents" },
  { key: "runs", labelKey: "computer.tabs.runs" },
  { key: "tunnels", labelKey: "computer.tabs.tunnels" },
  { key: "settings", labelKey: "computer.tabs.settings" }
];

export function ComputerDetailPanel({
  computer,
  runtimeCatalog,
  connectCommand,
  daemonVersion,
  targetDaemonVersion,
  api,
  onOpenAgent,
  onCreateAgent,
  onStartAll,
  onDeleteComputer,
  currentUserId = "",
  userIsAdmin = false
}: {
  computer: DaemonInventoryComputer;
  runtimeCatalog: readonly RuntimePreset[];
  connectCommand: string;
  daemonVersion?: string;
  targetDaemonVersion?: string;
  api?: ApiClient;
  onOpenAgent?: (agentId: string) => void;
  onCreateAgent?: () => void;
  onStartAll?: () => void;
  onDeleteComputer?: () => void;
  currentUserId?: string;
  userIsAdmin?: boolean;
}) {
  const { t } = useT();
  const [activeTab, setActiveTab] = useState<ComputerDetailTab>("overview");

  const online = isComputerOnline(computer);
  const installedKinds = useMemo(() => new Set(detectedRuntimeKinds(computer)), [computer]);
  const needsUpgrade = Boolean(targetDaemonVersion && daemonVersion && daemonVersion !== targetDaemonVersion);

  const header = (
    <StatusDot
      tone={online ? "online" : "offline"}
      label={online ? t("computer.online") : t("computer.offline")}
    />
  );

  const tabs = TAB_DEFS.map((entry) => ({ key: entry.key, labelKey: entry.labelKey }));

  const infoRows: KeyValue[] = [
    {
      label: t("computer.info.os"),
      value: computer.inventoryVersion || "—"
    },
    {
      label: t("computer.info.daemonVersion"),
      value: daemonVersion || "—",
      monospace: true,
      hint: needsUpgrade ? t("computer.info.updateAvailable") : undefined
    },
    {
      label: t("computer.info.lastHeartbeat"),
      value: computer.lastHeartbeatUnix
        ? new Date(computer.lastHeartbeatUnix * 1000).toLocaleString()
        : t("computer.info.noHeartbeat"),
      monospace: true
    }
  ];

  const runtimeChips =
    runtimeCatalog.length === 0 ? (
      <p className="detail-muted">{t("computer.info.runtimesEmpty")}</p>
    ) : (
      <div className="runtime-chip-row">
        {[...runtimeCatalog]
          .sort((left, right) => {
            const leftInstalled = installedKinds.has(left.kind) ? 0 : 1;
            const rightInstalled = installedKinds.has(right.kind) ? 0 : 1;
            if (leftInstalled !== rightInstalled) return leftInstalled - rightInstalled;
            return (left.displayName || left.kind).localeCompare(right.displayName || right.kind);
          })
          .map((preset) => {
            const installed = installedKinds.has(preset.kind);
            return (
              <span
                key={preset.kind}
                className={installed ? "runtime-chip runtime-chip-installed" : "runtime-chip runtime-chip-missing"}
              >
                {preset.displayName || preset.kind}
                {installed ? null : (
                  <span className="runtime-chip-meta">{t("computer.info.runtimesNotInstalled")}</span>
                )}
              </span>
            );
          })}
      </div>
    );

  const agentCount = computer.agents?.length ?? 0;

  return (
    <DetailShell
      eyebrow={t("computer.title")}
      title={computer.displayName || computer.hostname || computer.computerId}
      subtitle={computer.hostname && computer.displayName ? computer.hostname : undefined}
      headerActions={header}
      tabs={tabs}
      activeTab={activeTab}
      onChangeTab={setActiveTab}
      banner={!online ? <AlertPill tone="warning" label={t("agent.info.offlineBanner")} /> : null}
    >
      {activeTab === "overview" ? (
        <>
          <DetailSection title={t("computer.info.os")}>
            <KeyValueList items={infoRows} />
          </DetailSection>
          <DetailSection title={t("computer.info.detectedRuntimes")}>{runtimeChips}</DetailSection>
          <DetailSection
            title={t("computer.connect.title")}
            hint={t("computer.connect.description")}
            trailing={
              connectCommand ? (
                <CopyButton value={connectCommand} ariaLabel={t("computer.connect.copy")} />
              ) : null
            }
          >
            {connectCommand ? (
              <pre className="command-block" aria-label={t("computer.connect.title")}>
                <code>{connectCommand}</code>
              </pre>
            ) : (
              <p className="detail-muted">—</p>
            )}
          </DetailSection>
        </>
      ) : null}
      {activeTab === "agents" ? (
        <DetailSection
          title={t("computer.agents.heading")}
          trailing={
            <div className="detail-inline-actions">
              {onStartAll ? (
                <button className="ghost-button" type="button" onClick={onStartAll} disabled={agentCount === 0}>
                  {t("computer.agents.startAll")}
                </button>
              ) : null}
              {onCreateAgent ? (
                <button className="primary-button" type="button" onClick={onCreateAgent}>
                  {t("computer.agents.create")}
                </button>
              ) : null}
            </div>
          }
        >
          {agentCount === 0 ? (
            <EmptyState title={t("computer.agents.empty")} />
          ) : (
            <ul className="agent-row-list" role="list">
              {computer.agents!.map((agent) => (
                <li key={agent.agentId}>
                  <button
                    type="button"
                    className="agent-row"
                    onClick={() => onOpenAgent?.(agent.agentId)}
                    aria-label={`Open ${agent.displayName || agent.name || agent.agentId}`}
                  >
                    <StatusDot
                      tone={agent.status === "running" ? "running" : agent.status === "error" ? "error" : "idle"}
                      label={agent.displayName || agent.name || agent.agentId}
                    />
                    {agent.runtimeKind ? <span className="agent-row-runtime">{agent.runtimeKind}</span> : null}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </DetailSection>
      ) : null}
      {activeTab === "runs" ? (
        api ? (
          <AgentRunsPanel api={api} computerId={computer.computerId} />
        ) : (
          <EmptyState title={t("computer.tabs.runs")} body={t("agent.activity.empty")} />
        )
      ) : null}
      {activeTab === "tunnels" ? (
        api ? (
          <TunnelsPanel
            api={api}
            computerId={computer.computerId}
            userIsAdmin={userIsAdmin}
            currentUserId={currentUserId}
          />
        ) : (
          <EmptyState title={t("computer.tabs.tunnels")} body={t("agent.activity.empty")} />
        )
      ) : null}
      {activeTab === "settings" ? (
        <DangerZone
          title={t("computer.actions.delete")}
          onConfirm={() => onDeleteComputer?.()}
          disabledReason={
            agentCount > 0 ? t("computer.actions.deleteBlocked")
            : !onDeleteComputer ? t("common.noPermission")
            : undefined
          }
        />
      ) : null}
    </DetailShell>
  );
}
