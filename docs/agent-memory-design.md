# Lightweight agent memory for nekode

Status: **active plan** · Date: 2026-05-10 · Replaces the earlier full
three-layer LCM proposal.

## Why not full LCM

Stella's LCM was evaluated and rejected:

- Stella is **AGPL-3.0**, nekode is **MIT**. Copying or linking the code
  would relicense nekode.
- Even rewriting LCM from scratch would land a 9-table schema + DAG
  compaction + summariser infrastructure that duplicates capabilities
  already provided by the wrapped runtimes (Claude Code / Codex /
  OpenCode manage their own context). nekode observes them from the
  outside; the most valuable LCM content ("I tried approach A, failed
  because X") isn't reachable from that vantage point.
- The three-layer model's real value is in L3 (consensus) and the
  structured-facts query pattern of L2. We can ship both without LCM.

## What we ship instead (three small features)

| Feature | Role | Scope | Size |
|---|---|---|---|
| Channel pinned decisions | L3 equivalent | channel | new table `channel_decisions` |
| Message kind tag | L2 equivalent | message | `kind` field on existing Message |
| Agent run archive | L1 equivalent | agent | new table `agent_runs` + FTS5 |

No new sub-systems, no new control planes. Every piece reuses an existing
nekode entity (channel / message / agent / computer).

## 1. Channel pinned decisions (with voting)

Persistent, structured record of architectural decisions inside a
channel. Supports a lightweight propose → vote → ratify lifecycle so
multi-agent workflows can reach consensus without a heavy L3 protocol.

### Lifecycle

```
proposed ──approve quorum──▶ ratified ──retire──▶ retired
    │
    └─reject quorum / proposer withdraw──▶ rejected
```

- Quorum default: **≥ 2 approving voters AND no standing reject**. Admin
  override: channel owner can force-ratify or force-retire with a
  reason.
- Voters are channel members (humans + agents). Each voter gets one vote.
  Changing vote before ratification is allowed; after ratification votes
  freeze.
- A ratified decision can be **superseded** by a new decision that
  references it, which flips the older one to `retired` automatically
  once the new one ratifies.

### Data

```sql
CREATE TABLE channel_decisions (
  id            TEXT PRIMARY KEY,     -- ULID
  target        TEXT NOT NULL,        -- channel target (#general, etc.)
  title         TEXT NOT NULL,
  body          TEXT NOT NULL,
  status        TEXT NOT NULL CHECK (status IN
                   ('proposed','ratified','rejected','retired')),
  proposer_id   TEXT NOT NULL,
  created_unix  INTEGER NOT NULL,
  ratified_unix INTEGER,
  retired_unix  INTEGER,
  retired_by    TEXT,
  retire_reason TEXT,
  supersedes    TEXT                  -- prior decision id (optional)
);
CREATE INDEX idx_channel_decisions_target
  ON channel_decisions(target, status, created_unix);

CREATE TABLE channel_decision_votes (
  decision_id  TEXT NOT NULL REFERENCES channel_decisions(id) ON DELETE CASCADE,
  voter_id     TEXT NOT NULL,         -- user id or agent id
  voter_kind   TEXT NOT NULL CHECK (voter_kind IN ('human','agent')),
  decision     TEXT NOT NULL CHECK (decision IN ('approve','reject','abstain')),
  voted_unix   INTEGER NOT NULL,
  reason       TEXT,
  PRIMARY KEY (decision_id, voter_id)
);
```

### API

```
GET    /api/channels/:target/decisions                   # list (proposed + ratified by default)
POST   /api/channels/:target/decisions                   # body: { title, body, supersedes? }
POST   /api/decisions/:id/vote                           # body: { decision: approve|reject|abstain, reason? }
POST   /api/decisions/:id/ratify                         # admin override / auto-called when quorum met
POST   /api/decisions/:id/retire                         # body: { reason? }
GET    /api/decisions/:id/votes                          # list of votes with voter + timestamp
```

The `POST /vote` endpoint is the hot path: after writing the vote it
re-evaluates quorum in the same transaction and flips status to
`ratified` automatically when the rule is satisfied. No background
worker needed.

### UI

Channel settings drawer grows a **Decisions** tab with three sections:

- **Proposed** — current drafts with a vote bar (approvals / rejections /
  needed) and your own vote button.
- **Ratified** — accepted decisions, newest first.
- **Retired / Rejected** — collapsed by default.

Each row: title · body (markdown) · proposer · vote tally · ratify or
retire link (admin only for forced actions).

Compose UX: from a normal message, a kind-"decision" row (see §2)
picks up an inline "Promote to proposal" button that opens the proposal
modal with the message body pre-filled.

### Agent access

Agents use the same REST endpoints through the runtime adapter's memory
tool:

```
nekode-memory-list-decisions   --target <channel> [--status ratified]
nekode-memory-propose-decision --target <channel> --title "..." --body "..."
nekode-memory-vote-decision    --id <decision-id> --decision approve [--reason "..."]
```

This lets agents both read ratified constraints and participate in the
voting process when the operator configures them as channel members.

## 2. Message kind tag

Messages today carry only `content` + `target` + `author`. Add a `kind`
enum so senders can classify a message, and readers can filter.

### Data

```sql
-- extend existing messages table
ALTER TABLE messages ADD COLUMN kind TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_messages_kind ON messages(target, kind, created_unix);
```

Allowed values: `note` (default / no tag), `decision` (proposed decision,
candidate for promotion to channel_decisions), `blocker` (I'm stuck on
X), `status` (progress update). Unknown values are rejected by the API.

### API

Extend `SendMessageRequest` with `kind`. No compat shim: clients
without a kind field send "note" explicitly.

### UI

- Composer grows a compact kind selector (icon-only, tooltip labelled)
  with four chips: note / decision / blocker / status.
- Message row renders a small chip for non-note kinds, coloured to match
  the semantic tone.
- MessagesPanel header adds filter chips: "All · Decisions · Blockers ·
  Status". Selecting filters the rendered stream client-side.

### Agent access

`kind` is set on `SendMessageRequest` from daemon side when the runtime
wraps the outgoing message. Runtime adapter exposes a CLI flag
`--kind decision` for agents that want to mark something explicitly.

## 3. Agent run archive

Every agent session generates a stream of lifecycle events (tool calls,
errors, final output, exit). Archive them, search them, show them on
the Computer detail Runs tab.

### Data

```sql
CREATE TABLE agent_runs (
  id            TEXT PRIMARY KEY,     -- ULID, generated by daemon
  agent_id      TEXT NOT NULL,
  computer_id   TEXT NOT NULL,
  started_unix  INTEGER NOT NULL,
  ended_unix    INTEGER,              -- null while running
  exit_code     INTEGER,
  summary       TEXT,                 -- short one-liner after end
  error         TEXT,
  event_count   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_agent_runs_agent ON agent_runs(agent_id, started_unix DESC);
CREATE INDEX idx_agent_runs_computer ON agent_runs(computer_id, started_unix DESC);

CREATE TABLE agent_run_events (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id        TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  at_unix_nano  INTEGER NOT NULL,
  phase         TEXT NOT NULL CHECK (phase IN
                   ('start','tool_call','tool_result','error','output','end')),
  summary       TEXT NOT NULL,
  payload_json  TEXT
);
CREATE INDEX idx_agent_run_events_run ON agent_run_events(run_id, at_unix_nano);

-- FTS5 for "find runs where the agent did X"
CREATE VIRTUAL TABLE agent_run_events_fts USING fts5(
  summary, payload_json, content='agent_run_events', content_rowid='id'
);
```

### Proto

New RPC, new message — proto file lives at
`proto/nekode/daemon/v1/agent_runs.proto`:

```proto
syntax = "proto3";
package nekode.daemon.v1;

message AgentRunEvent {
  string run_id      = 1;
  string agent_id    = 2;
  string computer_id = 3;
  enum Phase {
    PHASE_UNSPECIFIED   = 0;
    PHASE_START         = 1;
    PHASE_TOOL_CALL     = 2;
    PHASE_TOOL_RESULT   = 3;
    PHASE_ERROR         = 4;
    PHASE_OUTPUT        = 5;
    PHASE_END           = 6;
  }
  Phase  phase         = 4;
  int64  at_unix_nano  = 5;
  string summary       = 6;
  bytes  payload_json  = 7;
  // populated only on PHASE_END
  int32  exit_code     = 8;
  string error_message = 9;
}

message ReportAgentRunResponse {
  int64 persisted_count = 1;
  int64 dropped_count   = 2;
}

service AgentRunsService {
  rpc ReportAgentRun(stream AgentRunEvent) returns (ReportAgentRunResponse);
}
```

Existing `AgentStatusSnapshot` grows:

```proto
message AgentStatusSnapshot {
  // ... existing fields
  string current_run_id        = 30;  // empty when idle
  string last_run_id            = 31;
  int64  current_run_started_unix = 32;
}
```

### Daemon

`internal/runtimeadapter` already intercepts runtime lifecycle (spawn /
exit / tool calls). Wire a thin `RunRecorder` that:

1. Generates `run_id` at spawn.
2. Buffers `AgentRunEvent` messages in-memory.
3. Flushes every 500 ms or on PHASE_END to the server's
   `ReportAgentRun` stream.
4. Reconnects and replays on transient failures.

No local SQLite on the daemon — server is authoritative. If the server
is unreachable for > 60 s we start dropping old events (prefer the
latest PHASE_END over stale tool_calls) and increment `dropped_count`.

### Server

`internal/daemonrpc` gains `ReportAgentRun` handler:

- Validates `run_id` is server-new or matches an open run for the
  agent.
- Persists events to `agent_run_events` via ent.
- Updates `agent_runs.event_count` every N events.
- On PHASE_END: fills `ended_unix`, `exit_code`, `error`, computes
  `summary` from the final output.

HTTP side, new endpoints on `internal/server`:

```
GET /api/agents/:agentId/runs?limit=50
GET /api/computers/:computerId/runs?limit=50
GET /api/runs/:runId
GET /api/runs/:runId/events
GET /api/runs/search?q=...  (FTS5 over agent_run_events_fts)
```

### UI

Two places consume this:

- **Computer detail → Runs tab** (already shells in M3). Pulls
  `/api/computers/:id/runs`, shows one row per run with agent name,
  start/end, exit status. Row expands to show the event timeline.
- **Agent detail → Activity tab** (already shells in M3). Pulls
  `/api/agents/:id/runs` — same layout, agent-scoped.

Search: a single search box at the Runs tab level queries
`/api/runs/search` and returns highlighted excerpts.

## 4. Delivery order

1. [x] Design doc (this file)
2. proto file + `buf generate`
3. ent schema + migration
4. storage methods
5. server HTTP endpoints + daemonrpc handler
6. daemon-side `RunRecorder` + runtime hook
7. web: channel decisions UI
8. web: message kind selector + filter
9. web: Runs tab population + search
10. codex review, fix, codex sign-off

Steps 2–6 land the backend (no user-visible change until the UI comes
along); 7–9 are the user-facing payoff; 10 is the gate.

## 5. Non-goals

- No DAG summarisation. If someone needs a digest of an agent's last 50
  runs, we add a single `summary` column that stores the runtime's own
  output rather than re-summarising from events.
- No vector search in v1. FTS5 is enough.
- No cross-computer sync protocol. Server is authoritative; daemons
  only write. Reads go through the same HTTP endpoints every client
  uses.
- No complex voting rules. Quorum is fixed at ≥ 2 approvals with no
  standing reject; admin can force-ratify or force-retire. If teams
  later want configurable quorum (majority, unanimous, weighted), the
  rule lives in a single server-side evaluator and the schema doesn't
  change.
