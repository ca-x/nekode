import type { DaemonInventoryComputer } from "../../types";
import { useT } from "../../i18n/use-t";
import { StatusDot, heartbeatTone } from "../_shared/status-dot";
import { EmptyState } from "../_shared/empty-state";
import { isComputerOnline } from "./computer-utils";
import { Plus } from "lucide-react";

/**
 * Context-rail list of enrolled computers. Selecting a row calls onSelect so
 * the host can navigate to the detail panel via hash routing.
 */
export function ComputersListPanel({
  computers,
  selectedId,
  onSelect,
  onAddComputer
}: {
  computers: readonly DaemonInventoryComputer[];
  selectedId?: string;
  onSelect: (computerId: string) => void;
  onAddComputer: () => void;
}) {
  const { t } = useT();

  return (
    <div className="computers-list">
      <header className="computers-list-header">
        <h3>{t("computer.title")}</h3>
        <button
          type="button"
          className="icon-button"
          onClick={onAddComputer}
          aria-label={t("computer.addComputer")}
          title={t("computer.addComputer")}
        >
          <Plus size={16} aria-hidden="true" />
        </button>
      </header>
      {computers.length === 0 ? (
        <EmptyState
          title={t("computer.listEmpty")}
          action={
            <button type="button" className="primary-button" onClick={onAddComputer}>
              <Plus size={14} aria-hidden="true" />
              {t("computer.addComputer")}
            </button>
          }
        />
      ) : (
        <ul className="computers-list-items" role="list">
          {computers.map((computer) => {
            const active = computer.computerId === selectedId;
            const online = isComputerOnline(computer);
            return (
              <li key={computer.computerId}>
                <button
                  type="button"
                  className={active ? "computers-list-item is-active" : "computers-list-item"}
                  onClick={() => onSelect(computer.computerId)}
                  aria-current={active ? "true" : undefined}
                >
                  <StatusDot
                    tone={heartbeatTone(computer.lastHeartbeatUnix)}
                    label={computer.displayName || computer.hostname || computer.computerId}
                  />
                  <span className="computers-list-meta">
                    {online ? t("computer.online") : t("computer.offline")}
                    {computer.hostname ? ` · ${computer.hostname}` : ""}
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
