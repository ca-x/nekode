import type { ReactNode } from "react";
import type { MessageKey } from "../../i18n/types";
import { useT } from "../../i18n/use-t";

export type TabDescriptor<Key extends string> = {
  key: Key;
  labelKey?: MessageKey;
  /** Fallback label when labelKey isn't supplied (reserved for dynamic tabs). */
  label?: string;
  /** Optional numeric badge shown next to the label. */
  count?: number;
  /** Optional decoration node, e.g. an "experimental" chip. */
  trailing?: ReactNode;
};

/**
 * In-page tab strip used by ComputerDetailPanel / AgentDetailPanel.
 *
 * Keeps a consistent visual treatment for Overview/Agents/Runs/Settings and
 * Profile/DMs/Reminders/Workspace/Activity. The component itself is
 * presentational — callers manage active state and routing.
 */
export function TabBar<Key extends string>({
  tabs,
  active,
  onChange,
  ariaLabel
}: {
  tabs: readonly TabDescriptor<Key>[];
  active: Key;
  onChange: (key: Key) => void;
  ariaLabel?: string;
}) {
  const { t } = useT();
  return (
    <div className="tab-bar" role="tablist" aria-label={ariaLabel}>
      {tabs.map((tab) => {
        const label = tab.labelKey ? t(tab.labelKey) : tab.label ?? tab.key;
        const isActive = tab.key === active;
        return (
          <button
            key={tab.key}
            className={isActive ? "tab-bar-tab is-active" : "tab-bar-tab"}
            type="button"
            role="tab"
            aria-selected={isActive}
            aria-controls={`tab-panel-${tab.key}`}
            id={`tab-${tab.key}`}
            onClick={() => onChange(tab.key)}
          >
            <span className="tab-bar-label">{label}</span>
            {typeof tab.count === "number" && tab.count > 0 ? (
              <span className="tab-bar-count tabular-nums" aria-hidden="true">
                {tab.count}
              </span>
            ) : null}
            {tab.trailing}
          </button>
        );
      })}
    </div>
  );
}
