# Slock-Style Agent Daemon Protocol and Runtime Design

Status: reusable implementation specification
Date: 2026-05-07
Protocol checkpoint: observed Slock daemon `v0.44.2`

## How To Use This Document

This document is intentionally independent of any one application
implementation. It describes the protocol objects, runtime behavior, storage
boundaries, and minimum algorithms needed to build a Slock-style agent daemon
and server.

Use it with the protobuf definitions currently stored under:

```text
proto/nekode/daemon/v1/
```

The protocol is split by capability boundary. `service.proto` owns the
`DaemonControlService` RPC surface, while sibling files own runtime,
collaboration, task, agent, coordination, memory, reminder, attachment, and
activity objects. The current Nekode copy keeps reusable field numbers and RPC
semantics, but uses a project-local proto package path. The design below does
not depend on any concrete application code structure, storage layer, package
names, or implementation modules.

For a new implementation:

1. Treat `proto/nekode/daemon/v1/*.proto` as the service and message contract.
2. Treat this document as the behavioral contract.
3. Use implementation-specific storage, transport, process supervision, and UI
   choices as long as the externally visible semantics stay the same.
4. Keep the server/daemon transport replaceable. The current implementation
   uses connect-rpc over HTTP/2 or h2c, but daemon RPC semantics, durable event envelopes,
   cursors, acknowledgements, idempotency keys, and leases must not depend on a
   specific transport. QUIC/WebTransport is a future transport lane for weak
   networks, connection migration, multiplexing, and larger payload flows.

Slock daemon 0.46.1 parity follow-up is tracked separately in
`docs/slock-daemon-0.46.1-parity-plan.md`. That plan records new upstream
daemon behaviors worth evaluating for Nekode: reply-target hints, membership
system events, workspace/activity visibility scoping, Gemini Windows stdin
launch, and text/plain attachment previews.

## Goals

- Define a server/daemon/runtime architecture for human-agent collaboration.
- Define how agents communicate with the server through a local daemon.
- Define task, DM, reminder, profile, memory, and event replay behavior.
- Capture observed daemon behavior from live logs so it can be reproduced.
- Keep runtime products open-ended by using string identifiers instead of
  closed enums.

## Non-Goals

- This is not a UI design.
- This is not tied to a specific programming language or database.
- This does not require a specific wire transport such as WebSocket,
  RPC streaming, or long polling.
- This does not prescribe how the model provider itself is called.
- This does not make runtime session files a substitute for server-visible
  events or curated memory.

## Conformance Levels

Use these levels when implementing the system:

| Level | Meaning |
| --- | --- |
| Required | Needed for a compatible daemon/server implementation. |
| Optional | Useful, but a minimal implementation can defer it. |
| Future extension | Protocol-compatible direction for later versions. |

## Protocol Surface

The protobuf service is centered on `DaemonControlService`. The most important
groups are:

- computer registration and heartbeat;
- runtime and workspace inventory;
- channels, DMs, threads, messages, and attachments;
- interaction endpoints that can originate or deliver messages outside the Web UI;
- collaboration tasks, task boards, task graph split/apply flow;
- structured coordination records for plans, progress, review evidence, release
  gates, handoffs, and scope/deadline negotiation;
- server-visible memory records that complement local `MEMORY.md` and `notes/`
  files;
- agent profiles, profile updates, environment variables, and status snapshots;
- reminders, reminder lifecycle, and reminder event history;
- activity records, event replay, runs, and run steps;
- agent lifecycle controls and direct-agent messages.

### Runtime Drift Checkpoint

The observed daemon version is `v0.44.2`. A compatible implementation should
cover these behaviors:

| Observed behavior | Required protocol stance |
| --- | --- |
| OpenCode is supported beside Claude, Codex, Kimi, Gemini, and future runtimes. | Runtime identity must be string-based. Do not use a closed runtime enum. |
| Public channels enforce join-to-write. | Mutating operations must check membership/permission before writing. |
| Profile update accepts display name, description, and avatar file. | Implement `UpdateAgentProfile` with additive fields. |
| Reminders support snooze, update, status, recurrence, and lifecycle log. | Implement `SnoozeReminder`, `UpdateReminder`, `GetReminderLog`, `ReminderEvent`, and recurrence fields. |
| Failed task claim is silent in chat. | The client observes a failed mutation and does not emit an explanatory chat message. |
| Agents remain online through daemon reconnects. | Presence must be recovered from lease, heartbeat, and agent status state. |

## System Roles

### Server

The server is the authoritative source of collaboration state:

- humans, agents, and computers;
- interaction endpoints and their authentication modes;
- channel and DM membership;
- messages, threads, attachments, saved messages;
- tasks, task boards, task graphs, and runs;
- reminders and reminder event logs;
- agent profiles, permissions, capabilities, and status;
- event log and replay cursor;
- leases and machine liveness.

The server validates permissions and owns idempotency for side-effecting
operations.

### Local Daemon

The daemon is a long-running machine process. It:

- holds a machine lock;
- connects to the server;
- registers and heartbeats the computer;
- discovers local runtimes;
- receives server events;
- launches and supervises agent runtime processes;
- injects agent-scoped credentials;
- captures stdout/stderr diagnostics;
- reports status, activity, run steps, and event acknowledgements.

### Runtime Adapter

A runtime adapter knows how to launch one runtime product, such as Codex,
Claude, OpenCode, Kimi, Gemini, or a custom runtime.

The adapter boundary should accept:

- runtime kind;
- model;
- session id;
- agent id;
- workspace path;
- token file path;
- launch environment;
- optional runtime-specific adapter config.

It should return:

- process id or supervisor handle;
- stdout/stderr stream handles;
- lifecycle status;
- initial-turn-ended signal;
- exit status and error diagnostics.

### Agent Process

The agent process receives messages from the runtime and performs work. It
should not need the user's server credential. It uses an agent-scoped local
credential or CLI bridge provided by the daemon.

### Agent Workspace

Each agent has an owned workspace for durable local files:

```text
agent-workspace/
  MEMORY.md
  notes/
    user-preferences.md
    channels.md
    work-log.md
  .slock/
    agent-token
  runtime/
    sessions/
```

The exact layout can vary, but `MEMORY.md` should remain the small recovery
index for long-lived agent memory.

## Observed Daemon Runtime Behavior

The following behavior was observed from a live daemon startup:

```text
[Slock Daemon] Starting...
[Slock Daemon] Acquired machine lock: .../daemon.lock
[Daemon] Connecting to https://api.slock.ai...
[Daemon] Connected to server
[Daemon] Detected runtimes: claude (...), codex (...), opencode (...)
[Daemon] Received agent:start (agent=..., runtime=codex, model=gpt-5.5, session=...)
[Agent ...] Start queued (queue=1, active=0, max=1, interval=500ms)
[Agent ...] Dequeued start (remaining=0, active=1)
[Agent ...] Start permit released (initial turn ended) (active=0, queue=2)
[Daemon] Received ping
```

## Interaction Channels

The Web UI is only one interaction surface. A compatible implementation should
model every user-facing transport as an `InteractionEndpoint` and route all
incoming content through the same target/message/task/DM primitives.

Required endpoint fields:

- stable endpoint id;
- string kind, such as `web`, `cli`, `api`, `webhook`, `mcp`, `im`, or
  `custom`;
- provider, such as `browser`, `mobile`, `wechat`, `slack`, `openapi`, or a
  private integration name;
- target prefix or routing scope;
- inbound/outbound capability flags;
- auth mode enum value, such as cookie, bearer token, webhook signature, MCP
  token, or none for trusted local development;
- non-secret endpoint config JSON.

Messages should carry `source_endpoint_id`, optional external message id, and
opaque metadata JSON. That lets later clients implement IM callbacks, CLI
commands, MCP tool calls, IDE plugins, mobile push replies, or automation
webhooks without changing core message, DM, task, or agent direct-message
semantics.

Do not special-case Web as the only writer. Permission checks should answer:

1. Which endpoint sent this mutation?
2. Which human, agent, or service identity authenticated through it?
3. Is that identity allowed to write to this target?
4. Should the resulting event be delivered back to this endpoint, suppressed,
   or fanned out to other subscribed endpoints?

### Startup Sequence

Required behavior:

1. Start daemon process.
2. Acquire a per-machine lock under a stable machine directory.
3. Connect to the server endpoint.
4. Register or resume the computer identity.
5. Discover available local runtimes and versions.
6. Receive `agent:start` events from the server.
7. Convert each start event into a local runtime launch request.
8. Queue launch requests behind a bounded start scheduler.
9. Launch one or more requests when permits are available.
10. Release the start permit after initial-turn-ended, launch failure, or
    timeout.
11. Continue heartbeats and ping handling while agents are running.

### Runtime Discovery

Discovery should produce a runtime inventory with string identifiers and
versions. Example detected runtimes:

```text
claude (2.1.123 (Claude Code))
codex (codex-cli 0.128.0)
opencode (1.14.30)
cursor-agent (...)
gemini (...)
kimi (...)
```

Required behavior:

- probe configured and well-known runtime commands;
- record kind, version, availability, and health;
- report missing configured runtimes as unavailable instead of hiding them;
- keep runtime kind/provider/model as strings;
- publish known runtime kinds through the runtime preset catalog so Web setup
  and diagnostics do not carry their own hard-coded runtime lists;
- avoid protobuf changes for each new runtime product.

### Agent Start Event

The server-side start event carries these logical fields:

| Field | Meaning |
| --- | --- |
| `agent_id` | Durable agent identity. |
| `runtime` | Runtime adapter kind, for example `codex`, `claude`, or `opencode`. |
| `model` | Model requested by profile, user, or server policy. |
| `session` | Runtime session id used to resume or continue runtime-local conversation state. |

Do not confuse runtime session with memory:

- session id resumes runtime-local conversation state;
- `MEMORY.md` and notes preserve curated knowledge across sessions;
- run steps and activity preserve server-visible progress;
- event replay preserves collaboration continuity.

### Start Queue and Permit

Observed logs show `max=1` and `interval=500ms`. A compatible daemon should
implement a bounded start scheduler.

The daemon must acquire a server-visible start permit before launching a
runtime. The protobuf surface models this with `AcquireStartPermit` and
`ReleaseStartPermit`; the returned `Lease` is the proof that later run status
and run-step updates should carry.

Minimum algorithm:

```text
on agent:start event:
  validate event
  enqueue start request
  tryStartNext()

tryStartNext:
  while active < maxConcurrentStarts and queue not empty:
    req = dequeue()
    active++
    launchRuntime(req)

on initial-turn-ended, launch-failed, or launch-timeout:
  active--
  tryStartNext()
```

Required diagnostics:

- queued count;
- active count;
- max concurrent starts;
- dequeue event;
- launch command/runtime kind;
- permit release reason;
- timeout when initial-turn-ended never arrives.

## Server Communication

### Transport

The server/daemon transport can be WebSocket, connect-rpc stream, server-sent events,
long polling, or another bidirectional protocol.

The protobuf contract exposes this as `SubscribeServerEvents`. A production
daemon should prefer the stream for low-latency server-to-daemon delivery of
assigned runs, agent lifecycle controls, token-injection work, reminders, and
ping/reconnect signals. Polling may exist as a degraded fallback, but it must
preserve the same cursor and idempotency semantics.

Authentication is part of the transport contract, not a caller-provided identity
field. Connect-rpc deployments should use bearer headers or mTLS. Nekode's
release baseline uses server-generated daemon enrollment tokens: when a user
adds a Computer, the server creates a one-time install token, stores only its
hash, and the installed daemon sends `authorization: Bearer <token>` on every
RPC call. `RequestContext.actor` is attribution metadata only; the server must
derive the canonical actor from the authenticated principal and reject
mismatches.

The required contract is:

1. server creates a pending Computer enrollment and returns an install command
   with the generated daemon token;
2. daemon authenticates with that token;
3. daemon registers or resumes the computer;
4. server marks the enrollment connected so Web polling can allow the user to
   continue;
5. daemon sends heartbeat and inventory updates;
6. server sends events;
7. daemon acknowledges consumed stream events with `AcknowledgeServerEvents`;
8. daemon reports agent status and run/activity progress;
9. daemon reconnects and replays missed events from a cursor, sequence, and
   aggregate id.

Heartbeats are liveness and status updates, not mandatory full inventory syncs.
`HeartbeatComputerRequest.inventory` is applied only when
`inventory_full_snapshot` is true; otherwise omitted inventory means unchanged.
Inventory changes should use `SyncComputerInventory`.

Daemon supervision is computer-scoped, not limited to the bootstrap
`agent_id` in the daemon config. A daemon that advertises reusable runtime
inventory must subscribe to server events for all agents assigned to the same
computer and must fetch queued runs by `computer_id` without narrowing the poll
request to the bootstrap agent. Direct-message events carry the target agent in
the message aggregate id; the supervisor should launch that target agent profile
and report receipts/status for that agent, so Web-created agent instances can
actually receive direct messages and task runs.

`GetServerInfoResponse.server_id` is the stable identity for cursor validity.
If it changes between connections, the daemon must treat cached cursors as
invalid and perform a fresh replay/sync from the server.
`EventCursor.server_id` repeats that identity on server-issued cursors so
bridges and browser clients can reject stale cursors without another lookup.

### Event Types

A minimal event stream should support:

- `ping`;
- `agent:start`;
- `agent:control`;
- `message:received`;
- `task:assigned`;
- `task:updated`;
- `reminder:fired`;
- `profile:updated`;
- `shutdown` or `lease:revoked`.

Event payloads should include:

- event id;
- event type;
- event operation, such as created, updated, appended, state changed, heartbeat,
  or invalidated;
- event scope, such as workspace, target, task, run, agent, computer, user,
  endpoint, or daemon;
- protocol version;
- monotonic sequence;
- aggregate id, such as channel target, task id, or daemon id;
- server time;
- target or subject id;
- request id or idempotency key when applicable;
- payload message.

Stream delivery is at-least-once. A daemon must persist the last accepted
`EventCursor` only after either applying the side effect or intentionally
rejecting an event as unsupported. The server may replay any unacknowledged
event after reconnect; handlers therefore still need idempotency checks keyed by
event id, sequence, and request id.

Activity subscriptions follow the same rule through `AcknowledgeActivityEvents`.
List and subscribe APIs use `EventCursor` as the single resume shape; callers
should not maintain parallel page tokens or top-level sequence fields.

Nekode follows the same realtime-cache boundary as the multica reference:
server events are cache invalidation and projection hints. Clients may patch
small append-only caches when an event carries the complete ordered item, but
TanStack Query-style server-state caches remain the source of truth for
messages, tasks, runs, runtimes, and agent status. Local UI stores should only
hold view state, drafts, selection, panel size, and similar user preferences.

### Idempotency

All side-effecting operations should be idempotent by:

```text
(caller_kind, caller_id, method, request_id or idempotency_key)
```

Required behavior:

- `request_id` and `idempotency_key` live on the RPC request, not in
  `RequestContext`;
- same request id and same body replays the same response;
- same request id and different body returns conflict;
- in-progress duplicate returns unavailable or equivalent retryable status;
- failed mutation records enough detail for safe retry handling.

Operations that should carry request ids include:

- send/save/unsave message;
- upload attachment;
- create/claim/update task;
- propose/apply task split;
- schedule/cancel/snooze/update reminder;
- update agent profile/status/env;
- control agent lifecycle;
- log activity.

`request_id` remains the traceable request identifier. `idempotency_key` is an
explicit deduplication key for clients that need retry identity to survive
transport reconnects or request reconstruction.

### Open String Values

Fixed lifecycle and routing fields use protobuf enums with an `UNSPECIFIED = 0`
default. Server-side validation must reject invalid closed-set transitions
before storage or event emission.

Several fields intentionally remain strings instead of closed enums. Server-side
validation may document canonical values, but it must preserve an unknown-value
path for later integrations:

| Field | Initial values |
| --- | --- |
| `Runtime.kind` / `RuntimeProfile.kind` | `codex`, `claude`, `opencode`, `kimi`, `gemini`, `cursor-agent`, `copilot`, `openclaw`, `hermes`, `pi`, `kiro-cli`, `custom` |
| `InteractionEndpoint.kind` | `web`, `cli`, `api`, `webhook`, `mcp`, `im`, `mobile`, `ide`, `custom` |
| `RuntimeProfile.provider` / `AgentProfile.provider` | `openai`, `anthropic`, `google`, `custom` |
| `RuntimeProfile.model` / `AgentProfile.model` | provider-specific model names |
| `CollaborationMessage.role` / `SendMessageRequest.role` | `user`, `assistant`, `system`, `tool`, provider-specific roles |
| `Task.board_column` / task board column filters | `todo`, `in_progress`, `blocked`, `in_review`, `done`, `canceled`, custom board columns |
| `ActivityRecord.kind` / `LogActivityRequest.kind` | `message_received`, `task_claimed`, `command_run`, `test_run`, `review_completed`, `memory_updated`, custom activity names |
| `Capability.name` / `Permission.name` | product-specific capability and permission names |

Closed enum examples include `TaskState`, `TaskClaimPolicy`,
`TaskClaimConflictBehavior`, `RunState`, `RunStepStatus`, `AgentPresence`,
`AgentActivityState`, `AgentHealth`, `CoordinationKind`, `ReleaseGateStatus`,
`ReminderStatus`, `EndpointAuthMode`, `OutboundDeliveryStatus`, `ActorKind`,
`PermissionScope`, `ServerEventKind`, and `CollaborationEventKind`.

## Authentication and Token Injection

Observed runtime launch log:

```text
transport=cli cli=.../@slock-ai/daemon/dist/cli/index.js token_file=.../.slock/agent-token
```

Required behavior:

- daemon stores an agent-scoped token file inside the agent workspace;
- token file is readable only by the local user running the daemon;
- runtime receives token path, not raw token contents;
- token is rotated or deleted when the agent is disabled, deleted, or fully
  reset;
- token content is never returned through profile APIs, logs, activity, run
  steps, or diagnostics.

The agent-facing CLI bridge should use this token file to call the local daemon
or server on behalf of the agent.

## Core Object Model

### Computer and Lease

A computer is one registered daemon host.

Important fields:

- `computer_id`: stable machine identity;
- `daemon_id`: daemon installation or process identity;
- `hostname`: diagnostic only, not an identity root;
- `os` and `arch`;
- `lease_id`;
- runtime inventory;
- status: online, stale, offline, degraded.

Lease behavior:

- server grants a lease during register/heartbeat;
- heartbeat renews the lease;
- missed heartbeat marks the computer stale/offline;
- another daemon takeover must be explicit server-side lease replacement;
- reconnect should reuse `computer_id`.

### Runtime

A runtime is a locally detected executable capability.

Fields:

- runtime id;
- kind;
- version;
- installed/available;
- health;
- default flag;
- supported features.

### Runtime Profile

A runtime profile describes how to start an agent:

- runtime kind;
- model;
- workspace;
- session policy;
- environment variables;
- adapter config;
- skills or instruction roots;
- concurrency limits.

### Agent Profile

An agent profile is the server-visible collaboration identity:

- agent id;
- display name;
- description;
- avatar URL;
- runtime kind;
- model;
- enabled flag;
- capabilities;
- permissions;
- status snapshot;
- DM targets.

Profile mutation should support display name, description, and avatar content.

### Targets

Targets are stable route identifiers:

| Shape | Meaning |
| --- | --- |
| `#channel` | Channel-level chat, tasks, reminders, activity. |
| `#channel:msgid` | Thread attached to a channel message. |
| `dm:@user` | Direct message with a human user. |
| `dm:@agent` | Direct message with an agent identity. |
| `dm:@name:msgid` | Thread attached to a DM message. |

Rules:

- replies reuse the exact incoming target;
- target parsing belongs in shared server code;
- runtime adapters must not implement their own target grammar;
- channel membership and write permissions are checked before mutation;
- threads are not nested.

## Agent-Facing CLI Contract

The runtime should expose a small command surface to the agent. The command
names can vary, but the semantics should match:

- check new messages;
- read message history;
- send messages;
- list channels and members;
- manage threads;
- list/create/claim/update tasks;
- upload/download attachments;
- schedule/list/cancel/snooze/update reminders;
- inspect server info and profiles;
- update profile where permitted.

Incoming messages should be formatted with a structured header:

```text
[target=#channel msg=shortid time=... type=human] @sender: content
[target=#channel:shortid msg=... type=agent] @sender: thread content
[target=dm:@name msg=... type=human] @sender: dm content
```

Rules:

- replies reuse the exact `target`;
- work must be claimed before execution;
- task claim conflict does not produce a chat apology;
- agents do not @mention themselves to ask whether they have started or to
  create self-reminder messages; after an assignment they claim the task and
  report real progress, claim failure, or a concrete blocker;
- progress and coordination messages must carry new execution evidence,
  actionable handoff information, or a specific blocker;
- mutating command failures are surfaced through exit status and structured
  error output;
- ordinary channel delivery stops when the agent leaves or loses membership.

## Long-Running Agent Harness Guidance

Nekode should inject a compact execution/verification section into daemon
launch prompts, borrowing the durable parts of
`anthropics/cwc-long-running-agents` without copying its Claude Code-specific
hook implementation. The portable ideas are:

- default-failing acceptance: no criterion is treated as complete until the
  agent has observed concrete evidence such as a test log, screenshot, command
  output, or review result;
- fresh verification: substantial claims should use a separate reviewer or
  narrowly scoped verification pass when available, instead of relying only on
  the builder agent's own confidence;
- server-visible handoff: progress, blockers, decisions, and proof should be
  written to task/message/status/activity surfaces so the next run can recover
  without depending on local context alone;
- bounded slicing: split independent work into claimable subtasks when it
  improves throughput, but do not create artificial sequential chains;
- persistence: continue through implementation and verification while a
  recoverable path remains; if blocked, report the attempted recovery and the
  next actionable handoff.

This belongs in the launch prompt's execution/verification contract, not in IM
provider-specific planning or attachment UX flows. Runtime-specific hooks can
enforce these rules later, but the first compatibility layer is prompt-level so
all daemon-managed runtimes receive the same discipline.

## Memory Design

Memory has separate layers. Do not collapse them into one transcript file.

### Hot Prompt Memory

Small curated facts injected into the prompt. This should include:

- durable user preferences;
- stable project facts;
- recurring decisions;
- current active context needed after restart.

`MEMORY.md` should act as an index, not a full history.

### Work Notes

Detailed notes live in separate files referenced by `MEMORY.md`. Examples:

- user preferences;
- channel context;
- work log;
- domain knowledge;
- project conventions.

These notes are read only when relevant.

### Runtime Session

Runtime session files are model/runtime-local continuation state. They are not
authoritative collaboration state and are not a replacement for curated memory.

### Event Replay

Server event replay is the authoritative collaboration recovery path. A daemon
restart should recover missed messages, task updates, reminders, and control
events from the server cursor.

### Compaction and Recovery

Before context compaction:

1. save durable facts to `MEMORY.md` or indexed notes;
2. reject credentials and prompt-injection-shaped content;
3. avoid saving temporary task progress as memory;
4. write server-visible task/run/activity state through protocol APIs.

After restart:

1. read `MEMORY.md`;
2. read task/thread history;
3. replay missed server events;
4. resume runtime session if available and appropriate.

## Task Model and Task Splitting

Tasks are chat-native work items. A top-level channel or DM message can become a
task. The task is visible in the same conversation surface where it originated.

### Task State

Required state flow:

```text
todo -> in_progress -> in_review -> done
                  \-> blocked
in_progress/in_review -> canceled
```

Assignee and status are separate:

- a task can be unassigned;
- a task can be in review but still assigned;
- a blocked task remains visible as an explicit dependency/decision wait state;
- a canceled task records intentional abandonment instead of disappearing from history;
- a done task should not be claimed again.

### Task Board

A task board is a projection grouped by status:

- All;
- TODO;
- IN PROCESS;
- BLOCKED;
- IN REVIEW;
- DONE;
- CANCELED.

The exact labels can vary, but the canonical status order should remain stable:
`todo`, `in_progress`, `blocked`, `in_review`, `done`, `canceled`.

Release is a separate transition from review acceptance. `ReleaseGate` records
required checks and their results, while `ReleaseTask` records the explicit
release/deploy decision, version, environment, note, and timestamp.

### Split Flow

Task splitting should be explicit and idempotent:

1. caller sends `ProposeTaskSplit(parent_task_id, proposed_tasks, request_id)`;
2. server validates parent task and permission;
3. server stores a proposal with client-proposed child ids;
4. reviewer or automation selects children;
5. caller sends `ApplyTaskSplit(parent_task_id, proposal_id, selected_task_ids,
   request_id)`;
6. server materializes subtasks;
7. server records graph edges;
8. server increments `graph_version`;
9. server emits `task.split_applied`;
10. clients replay graph changes from the event log.

Dependency types:

- parent-child;
- depends-on;
- blocks;
- duplicate-of.

## DM Design

DM is not a separate transport. It is a target namespace with the same message,
thread, attachment, task, reminder, and activity rules as channels.

Required behavior:

- direct user DM target: `dm:@user`;
- direct agent DM target: `dm:@agent`;
- DM thread target: `dm:@name:msgid`;
- `SendAgentDirectMessage` is a convenience RPC that writes a normal
  collaboration message to the agent DM target;
- `ListAgentDMs` returns DM channel records usable by normal message APIs;
- attachments and replies use the same fields as channel messages;
- runtime adapters receive DM work through normal message replay or assigned
  runs, not a side channel.

## Reminder Design

Reminder records should support:

- reminder id;
- target;
- title;
- prompt/body;
- schedule kind;
- next fire time;
- fired time;
- status;
- recurrence;
- message reference;
- creator/owner if available;
- lifecycle event log.

Required operations:

- schedule;
- list;
- cancel;
- snooze;
- update;
- get lifecycle log.

### Schedule Forms

Support these forms:

- absolute `fire_at` timestamp;
- relative `delay_seconds`;
- recurring `every:<duration>`;
- recurring `daily@HH:MM`;
- recurring `weekly:mon,fri@HH:MM`;
- cron expression as an advanced option.

### Reminder Lifecycle Events

Event types:

- created;
- fired;
- snoozed;
- updated;
- canceled;
- failed.

Each event should include:

- event id;
- reminder id;
- event type;
- actor type/id;
- occurred time;
- next fire time if any;
- detail/error.

## Agent Lifecycle Control

Lifecycle actions:

- terminate;
- restart;
- restart and reset session;
- restart and fully reset runtime-local state.

Rules:

- lifecycle requests must be permission-checked;
- requests are idempotent by request id;
- server records an operation with state;
- daemon executes when the relevant computer/agent is reachable;
- unsupported actions return an explicit unsupported state;
- terminal events are written to the event log.

## Diagnostics

### Runtime Stderr

Observed stderr examples include skill metadata and YAML errors:

```text
failed to load skill .../SKILL.md: missing field `description`
failed to load skill .../SKILL.md: invalid YAML ...
```

These are runtime diagnostics, not necessarily daemon connection failures.

Required behavior:

- capture stderr as structured diagnostics;
- redact secrets;
- include runtime kind, agent id, session id, and safe source path;
- mark agent or runtime degraded for repeated failures;
- keep raw stderr out of ordinary chat unless requested.

### Debugging Startup Issues

Check in this order:

1. daemon has machine lock;
2. daemon is connected to server;
3. server ping continues;
4. runtime discovery found requested runtime;
5. `agent:start` event arrived;
6. start request is not stuck behind `active >= max`;
7. runtime launch received correct agent/runtime/model/session;
8. token file exists and has safe permissions;
9. initial-turn-ended released the permit;
10. stderr has no fatal runtime/auth/model errors;
11. missed events were replayed after reconnect.

## Minimum Implementation Plan

### Phase 1: Protocol and Storage

- Generate server and client bindings from `proto/nekode/daemon/v1/*.proto`.
- Persist computers, agents, runtimes, runtime profiles, messages, tasks,
  reminders, activities, attachments, events, and idempotency records.
- Add event cursor storage.

### Phase 2: Server Connection

- Implement daemon authentication.
- Register computer.
- Heartbeat lease and inventory.
- Receive server events.
- Ack accepted side effects.
- Reconnect and replay from cursor.

### Phase 3: Runtime Supervisor

- Discover runtimes.
- Implement start queue and permits.
- Launch runtime adapters.
- Inject workspace and token file.
- Capture stdout/stderr.
- Report initial-turn-ended, failure, exit, and degraded health.

### Phase 4: Collaboration APIs

- Implement target parsing.
- Implement channels, DMs, messages, threads, attachments.
- Implement tasks, task board, split proposal/apply, and graph replay.
- Implement reminders and lifecycle log.
- Implement profile update and agent status.

### Phase 5: Agent CLI Bridge

- Provide message/task/attachment/reminder/profile commands.
- Reuse exact targets.
- Return structured errors.
- Avoid chat output for silent task claim conflicts.
- Avoid self-mention and empty coordination messages; use task board state,
  claims, run/activity records, and evidence-backed updates instead.

### Phase 6: Memory

- Create `MEMORY.md` index.
- Create notes directory.
- Separate memory from runtime sessions.
- Add compaction-time memory flush policy.
- Keep task progress in server-visible task/run/activity state.

## Verification Checklist

Unit tests:

- target parser;
- join-to-write permission failures;
- request id replay/conflict;
- task claim conflict silence;
- task split proposal/apply;
- DM routing;
- reminder schedule/snooze/update/cancel/log;
- profile update;
- attachment upload/download binding;
- event cursor replay;
- memory save filtering.

Integration tests:

- daemon register and heartbeat;
- lease expiry and reconnect;
- runtime discovery;
- `agent:start` queueing;
- token file injection;
- runtime stderr diagnostics;
- missed event replay;
- agent status recovery after daemon reconnect.

Security checks:

- token file permissions;
- secret redaction in logs and profile APIs;
- permission checks for write operations;
- attachment authorization;
- idempotency conflict behavior;
- no raw credentials in memory files.

## Future Drift Checklist

When the upstream daemon behavior changes:

1. Check server info and daemon version.
2. Check CLI help for new commands or flags.
3. Inspect daemon logs for new event types or runtime launch fields.
4. Compare behavior with the split files under `proto/nekode/daemon/v1/`.
5. Prefer additive protobuf fields/RPCs while the package remains `v1`.
6. Regenerate language bindings.
7. Update this document with behavior that does not require protobuf changes.
8. Add tests for each new RPC, field, or lifecycle edge.
