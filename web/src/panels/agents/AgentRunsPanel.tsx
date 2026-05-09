import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ApiClient } from "../../api";
import type { AgentRun, AgentRunEvent, AgentRunPhase } from "../../types";
import type { MessageKey } from "../../i18n/types";
import { useFormat, useT } from "../../i18n/use-t";
import { EmptyState } from "../_shared/empty-state";
import { AlertPill } from "../_shared/alert-pill";

/**
 * AgentRunsPanel shows the archive for either a single computer or a
 * single agent (pass the corresponding filter). A search input runs
 * substring queries against event summaries + payload via
 * /api/agent-runs/search.
 */
export function AgentRunsPanel({
  api,
  agentId,
  computerId
}: {
  api: ApiClient;
  agentId?: string;
  computerId?: string;
}) {
  const { t } = useT();
  const fmt = useFormat();

  const [runs, setRuns] = useState<AgentRun[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [expandedRunId, setExpandedRunId] = useState<string>("");
  const [eventsByRun, setEventsByRun] = useState<Record<string, AgentRunEvent[]>>({});
  const debounceRef = useRef<number | null>(null);

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true);
      setError("");
      try {
        if (query.trim()) {
          const result = await api.searchAgentRuns({ q: query.trim(), agentId, computerId, limit: 50 });
          if (signal?.aborted) return;
          setRuns((result.items ?? []).map((hit) => hit.run));
        } else {
          const result = await api.listAgentRuns({ agentId, computerId, limit: 50 });
          if (signal?.aborted) return;
          setRuns(result.items ?? []);
        }
      } catch (err) {
        if (signal?.aborted) return;
        setError(err instanceof Error ? err.message : "failed to load runs");
      } finally {
        if (!signal?.aborted) setLoading(false);
      }
    },
    [agentId, api, computerId, query]
  );

  useEffect(() => {
    const controller = new AbortController();
    if (debounceRef.current) window.clearTimeout(debounceRef.current);
    debounceRef.current = window.setTimeout(() => {
      void load(controller.signal);
    }, 250);
    return () => {
      controller.abort();
      if (debounceRef.current) window.clearTimeout(debounceRef.current);
    };
  }, [load]);

  async function toggleRun(runID: string) {
    if (expandedRunId === runID) {
      setExpandedRunId("");
      return;
    }
    setExpandedRunId(runID);
    if (eventsByRun[runID]) return;
    try {
      const result = await api.getAgentRun(runID, true);
      setEventsByRun((current) => ({ ...current, [runID]: result.events }));
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to load events");
    }
  }

  const hasAnyRun = runs.length > 0;
  const totalEventCount = useMemo(
    () => runs.reduce((sum, run) => sum + run.eventCount, 0),
    [runs]
  );

  return (
    <section className="runs-panel" aria-label={t("runs.title")}>
      <div className="runs-search">
        <input
          type="search"
          placeholder={t("runs.searchPlaceholder")}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          aria-label={t("runs.searchPlaceholder")}
        />
      </div>
      {error ? <AlertPill tone="danger" label={error} /> : null}
      {loading ? <p className="detail-muted">{t("runs.loading")}</p> : null}
      {!loading && !hasAnyRun ? <EmptyState title={t("runs.title")} body={t("runs.empty")} /> : null}
      <ul className="runs-list" role="list">
        {runs.map((run) => {
          const status = runStatus(run);
          const expanded = expandedRunId === run.id;
          const events = eventsByRun[run.id] ?? [];
          return (
            <li key={run.id}>
              <button
                type="button"
                className="run-row"
                aria-expanded={expanded}
                aria-controls={`run-events-${run.id}`}
                onClick={() => void toggleRun(run.id)}
              >
                <div className="run-row-title">
                  <strong>{run.agentId || run.id}</strong>
                  {run.summary ? <span className="run-row-summary">{run.summary}</span> : null}
                </div>
                <span className="tabular-nums">{fmt.dateTime(run.startedUnix * 1000)}</span>
                <span className="tabular-nums">{formatDuration(run, fmt)}</span>
                <span className={`status-dot-label run-status-${status}`}>{t(runStatusLabelKey(status))}</span>
              </button>
              {expanded ? (
                <ul className="run-events-list" id={`run-events-${run.id}`} role="list">
                  {events.length === 0 ? (
                    <li className="detail-muted">{t("runs.loading")}</li>
                  ) : (
                    events.map((event) => (
                      <li key={event.id} className="run-event-row">
                        <span className="run-event-phase" data-phase={event.phase}>
                          {t(eventPhaseLabelKey(event.phase))}
                        </span>
                        <span>{event.summary || event.errorMessage || "—"}</span>
                      </li>
                    ))
                  )}
                </ul>
              ) : null}
            </li>
          );
        })}
      </ul>
      {hasAnyRun ? <p className="detail-muted tabular-nums">events: {totalEventCount}</p> : null}
    </section>
  );
}

function runStatus(run: AgentRun): "running" | "succeeded" | "failed" {
  if (run.endedUnix === 0) return "running";
  if (run.error || run.exitCode !== 0) return "failed";
  return "succeeded";
}

function runStatusLabelKey(status: "running" | "succeeded" | "failed"): MessageKey {
  switch (status) {
    case "running":
      return "runs.status.running";
    case "failed":
      return "runs.status.failed";
    case "succeeded":
    default:
      return "runs.status.succeeded";
  }
}

function eventPhaseLabelKey(phase: AgentRunPhase): MessageKey {
  switch (phase) {
    case "start":
      return "runs.eventPhases.start";
    case "tool_call":
      return "runs.eventPhases.tool_call";
    case "tool_result":
      return "runs.eventPhases.tool_result";
    case "error":
      return "runs.eventPhases.error";
    case "end":
      return "runs.eventPhases.end";
    case "output":
    default:
      return "runs.eventPhases.output";
  }
}

function formatDuration(run: AgentRun, fmt: ReturnType<typeof useFormat>): string {
  if (!run.endedUnix || run.endedUnix <= run.startedUnix) return "—";
  const seconds = run.endedUnix - run.startedUnix;
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  return fmt.number(seconds / 3600, { maximumFractionDigits: 1 }) + "h";
}
