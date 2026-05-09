import { useState, type ReactNode } from "react";
import type { DaemonAgentInstance, DaemonInventoryComputer, Reminder } from "../../types";
import { useT } from "../../i18n/use-t";
import type { MessageKey } from "../../i18n/types";
import { DetailShell } from "../_shared/detail-shell";
import { DetailSection } from "../_shared/detail-section";
import { KeyValueList, type KeyValue } from "../_shared/kv-list";
import { EmptyState } from "../_shared/empty-state";
import { AlertPill } from "../_shared/alert-pill";
import { DangerZone } from "../_shared/danger-zone";
import { StatusDot } from "../_shared/status-dot";
import { isComputerOnline } from "../computers/computer-utils";

type AgentDetailTab = "profile" | "dms" | "reminders" | "workspace" | "activity";

const TAB_DEFS: ReadonlyArray<{ key: AgentDetailTab; labelKey: MessageKey }> = [
  { key: "profile", labelKey: "agent.tabs.profile" },
  { key: "dms", labelKey: "agent.tabs.dms" },
  { key: "reminders", labelKey: "agent.tabs.reminders" },
  { key: "workspace", labelKey: "agent.tabs.workspace" },
  { key: "activity", labelKey: "agent.tabs.activity" }
];

export function AgentDetailPanel({
  agent,
  computer,
  dmPanel,
  reminders,
  activityPanel,
  onStart,
  onRestart,
  onDelete,
  onOpenDms,
  onOpenActivity
}: {
  agent: DaemonAgentInstance;
  computer: DaemonInventoryComputer | null;
  dmPanel?: ReactNode;
  reminders: readonly Reminder[];
  activityPanel?: ReactNode;
  onStart?: () => void;
  onRestart?: () => void;
  onDelete?: () => void;
  onOpenDms?: () => void;
  onOpenActivity?: () => void;
}) {
  const { t } = useT();
  const [activeTab, setActiveTab] = useState<AgentDetailTab>("profile");

  const computerOnline = computer ? isComputerOnline(computer) : false;

  const status = agent.status === "running" ? "running" : agent.status === "error" ? "error" : "idle";
  const headerActions = <StatusDot tone={status} label={agent.status || "idle"} />;

  const infoRows: KeyValue[] = [
    {
      label: t("agent.info.computer"),
      value: computer ? computer.displayName || computer.hostname || computer.computerId : "—"
    },
    {
      label: t("agent.info.runtime"),
      value: agent.runtimeKind || agent.provider || "—"
    },
    {
      label: t("agent.info.model"),
      value: agent.model || "—"
    }
  ];

  const remindersForAgent = reminders.filter((reminder) => {
    // Prefer a structured pointer if the backend adds one; otherwise parse
    // the target string. Agents always schedule with their own identity so
    // this is sufficient.
    const anyReminder = reminder as Reminder & { targetAgentId?: string };
    if (anyReminder.targetAgentId) return anyReminder.targetAgentId === agent.agentId;
    if (reminder.target && reminder.target === `dm:${agent.agentId}`) return true;
    return false;
  });

  return (
    <DetailShell
      eyebrow={t("agent.title")}
      title={agent.displayName || agent.name || agent.agentId}
      subtitle={agent.description}
      headerActions={headerActions}
      tabs={TAB_DEFS.map((entry) => ({ key: entry.key, labelKey: entry.labelKey }))}
      activeTab={activeTab}
      onChangeTab={setActiveTab}
      banner={!computerOnline ? <AlertPill tone="warning" label={t("agent.info.offlineBanner")} /> : null}
    >
      {activeTab === "profile" ? (
        <>
          <DetailSection title={t("agent.tabs.profile")}>
            <KeyValueList items={infoRows} />
          </DetailSection>
          <DetailSection
            title={t("agent.actions.start")}
            trailing={
              <div className="detail-inline-actions">
                {onStart ? (
                  <button className="primary-button" type="button" onClick={onStart}>
                    {t("agent.actions.start")}
                  </button>
                ) : null}
                {onRestart ? (
                  <button className="ghost-button" type="button" onClick={onRestart}>
                    {t("agent.actions.restart")}
                  </button>
                ) : null}
              </div>
            }
          >
            <p className="detail-muted">{agent.agentId}</p>
          </DetailSection>
          <DangerZone
            title={t("agent.actions.delete")}
            action={
              <button className="danger-button" type="button" onClick={onDelete} disabled={!onDelete}>
                {t("agent.actions.delete")}
              </button>
            }
          />
        </>
      ) : null}
      {activeTab === "dms" ? (
        dmPanel ?? (
          <EmptyState
            title={t("agent.tabs.dms")}
            body={`dm:${agent.agentId}`}
            action={
              onOpenDms ? (
                <button className="primary-button" type="button" onClick={onOpenDms}>
                  {t("agent.tabs.dms")}
                </button>
              ) : undefined
            }
          />
        )
      ) : null}
      {activeTab === "reminders" ? (
        remindersForAgent.length === 0 ? (
          <EmptyState title={t("agent.tabs.reminders")} body={t("agent.reminders.empty")} />
        ) : (
          <ul className="agent-reminder-list" role="list">
            {remindersForAgent.map((reminder) => (
              <li key={reminder.id} className="agent-reminder-row">
                <strong>{reminder.title || reminder.id}</strong>
                <span className="detail-muted">{reminder.status}</span>
              </li>
            ))}
          </ul>
        )
      ) : null}
      {activeTab === "workspace" ? (
        <EmptyState title={t("agent.tabs.workspace")} body={t("agent.workspace.empty")} />
      ) : null}
      {activeTab === "activity" ? (
        activityPanel ?? (
          <EmptyState
            title={t("agent.tabs.activity")}
            body={t("agent.activity.empty")}
            action={
              onOpenActivity ? (
                <button className="ghost-button" type="button" onClick={onOpenActivity}>
                  {t("agent.tabs.activity")}
                </button>
              ) : undefined
            }
          />
        )
      ) : null}
    </DetailShell>
  );
}
