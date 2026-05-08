# Protocol Capability Review

Status: adversarial review hardening complete
Date: 2026-05-08

## Purpose

This review checks whether the Nekode daemon protocol can express the
collaboration behavior observed in Slock-style work before daemon and Web
implementation continues.

It compares three inputs:

- observed Slock daemon `v0.44.2` behavior;
- real team behavior from recent multi-agent work;
- the current Nekode protobuf contract under `proto/nekode/daemon/v1/`.

## Protocol File Layout

The old single `daemon.proto` file has been split by capability boundary:

| File | Boundary |
| --- | --- |
| `common.proto` | shared primitives: capability, permission, lease, event cursor, env, skill |
| `runtime.proto` | computer registration, heartbeat, runtime inventory, workspace, runs |
| `collaboration.proto` | channels, threads, DMs, messages, saved messages, interaction endpoints |
| `task.proto` | tasks, task board, claim semantics, task graph and split/apply flow |
| `agent.proto` | agent profiles, roles, profile update, status, control, direct messages |
| `coordination.proto` | work plans, progress updates, evidence, release gates, handoffs, negotiations |
| `memory.proto` | server-visible memory records and agent memory sync boundary |
| `reminder.proto` | reminder schedule, snooze, update, recurrence, lifecycle log |
| `attachment.proto` | attachment metadata, upload, and retrieval |
| `activity.proto` | activity log and event replay |
| `service.proto` | `DaemonControlService` RPC surface |
| `daemon.proto` | historical entrypoint retained for documentation links |

All files keep package `nekode.daemon.v1` and Go package
`github.com/ca-x/nekode/gen/go/nekode/daemon/v1;daemonv1`.

## Slock.ai v0.44.2 Boundary Check

| Capability | Current protocol support |
| --- | --- |
| Multiple runtime products, including OpenCode | `Runtime.kind`, `RuntimeProfile.kind`, and agent runtime fields remain strings instead of closed enums. |
| Server connection, machine lock, registration, heartbeat | `RegisterComputer`, `HeartbeatComputer`, `SyncComputerInventory`, `Lease`, `ComputerInfo`, and `ComputerInventory`. |
| Server-to-daemon command/event delivery | `SubscribeServerEvents` streams `ServerEvent` envelopes for assigned runs, agent controls, messages, tasks, reminders, activity, MCP resource updates, and pings. |
| Runtime discovery and launch queue visibility | `Runtime`, `AcquireStartPermit`, `ReleaseStartPermit`, `AgentStatusSnapshot`, `Run`, `RunStep`, and activity records can report queued/running/blocked states. |
| Agent-scoped token/CLI bridge injection | Represented by `RuntimeProfile`, redacted `EnvVar` records, `Workspace`, and memory/workspace boundaries. |
| Public join-to-write and permission checks | `Permission`, `ChannelRecord`, `InteractionEndpoint`, and task/message mutation requests carry actor and endpoint ids. |
| Profile update for display name, description, avatar | `UpdateAgentProfileRequest` keeps these additive fields. |
| Reminder snooze/update/log | `SnoozeReminder`, `UpdateReminder`, `GetReminderLog`, `ReminderEvent`, and recurrence fields. |
| Reconnect-preserved online status | `Lease`, heartbeat, and `AgentStatusSnapshot` separate daemon liveness from agent activity. |
| Silent failed claim behavior | `ClaimCollaborationTaskRequest.silent_on_conflict` and response conflict fields make this a machine-visible client choice. |

## External Review Fixes

Two external reviews were incorporated before implementation resumed. Some
findings were already covered by existing fields (`request_id`, `Lease`,
`Runtime.capabilities`, `MemoryRecord.version/scope`, reminder recurrence and
timezone), but the following real gaps were fixed additively:

| Review concern | Protocol resolution |
| --- | --- |
| Server push / daemon pull missing | Added `SubscribeServerEvents` and `ServerEvent` in `service.proto`. |
| Stream ack semantics unclear | Added `AcknowledgeServerEvents`; stream delivery is at-least-once and cursor advancement happens after ack. |
| Idempotency naming inconsistent | Mutating requests carry top-level `request_id`/`idempotency_key`; `RequestContext` is trace/actor metadata only. |
| Agent start/control needs lease semantics | `ControlAgentRequest` now carries lease id and TTL; `ControlAgentResponse` returns a `Lease`. |
| Event replay needs sequence and version | `EventCursor`, `ActivityRecord`, and `CollaborationEvent` now include protocol version, sequence, and aggregate id fields. |
| Message/activity pagination needs cursor semantics | Message, activity, coordination, task, catalog, and run list requests/responses use `EventCursor` without parallel page-token aliases. |
| Release gate needs release confirmation | Added `ReleaseTask` and release fields on `Task`. |
| Webhook/IM outbound delivery needs tracking | Added `OutboundDeliveryRecord`, list, and retry RPCs. |
| MCP needs resource subscription semantics | Added MCP resource subscription/update messages and RPCs. |
| Claim lease needs renewal | Added `RenewTaskClaimLease`. |
| Permission needs scope | Added `Permission.scope`. |
| Handoff needs richer context | Added context task ids, file paths, and task graph references to `RoleHandoff`. |

## Multica Reference Follow-up

The mature multica reference reinforced three protocol gaps that matter before
Nekode leans on browser SSE and projection caches:

| Reference concern | Nekode protocol resolution |
| --- | --- |
| Realtime fanout needs explicit rooms/scopes, not only broad target strings | Added `EventScopeType` and `EventScope`; server and collaboration event streams can carry scope metadata and accept scope filters. |
| Frontend server-state caches need stable invalidation verbs | Added `EventOperation` to `ServerEvent` and `CollaborationEvent`; clients can invalidate by operation/scope while still treating payload + sequence as authoritative. |
| Cursor/projection validity should include server identity | Added `EventCursor.server_id` so clients can discard stale cursors and cache keys across server migrations. |
| Runtime recovery/retry needs protocol-visible metadata | Added run attempt, max attempts, parent run, failure reason, and last heartbeat fields to `Run`. |
| Slock daemon 0.46.0 marks channels public/private | Added `ChannelVisibility`, channel `joined`/`member_count`, and `ListChannelMembers`; private names, members, and content remain membership-gated. |
| Slock daemon 0.46.0 message search supports recent/relevance and sender handles | Added `SearchMessages`, `MessageSearchSort`, `sender_handle`, and canonical `Actor sender` filtering. |
| Board task lifecycle needs blocked/canceled states without overloading columns | Added `TASK_STATE_BLOCKED` and `TASK_STATE_CANCELED`; `board_column` remains the open UI projection. |
| Large attachments need URL flow | Added presigned upload/download URL fields while keeping bytes for small payloads. |
| Future field reuse needs visible guardrails | Added `reserved 1000 to 1999` ranges to long-lived messages for extension discipline. |

## Adversarial Hardening Pass

After the first external review fix, a cross-model adversarial pass found
additional protocol risks. The following findings were accepted before daemon
implementation:

| Finding | Resolution |
| --- | --- |
| Server stream needed explicit processing acknowledgement | Added `AcknowledgeServerEvents`; the server advances delivery cursors only after ack. |
| Request identity was duplicated in `RequestContext` and top-level fields | Removed request/idempotency fields from `RequestContext`; top-level request fields are canonical. |
| Cursor and page-token pagination overlapped | Removed page-token aliases and kept `EventCursor` as the only pagination/resume shape. |
| `GetServerInfo` could force huge catalog payloads | Catalogs are opt-in and paged; scalar server/protocol metadata remains cheap. |
| Heartbeat inventory was full and ambiguous | Heartbeat inventory is applied only when `inventory_full_snapshot` is true; `SyncComputerInventory` handles inventory changes. |
| Run mutation lacked lease proof | `UpdateRunStatus` and `AppendRunStep` now carry `lease_id`. |
| `Run.status` and `Run.state` were ambiguous | `Run.state` is canonical at field 7; the old duplicate name/field is reserved. |
| Task graph topology was duplicated on `Task` | Inline child/dependency lists were removed; `TaskGraphSnapshot` is authoritative. |
| Release state was duplicated on `Task` and `ReleaseGate` | `Task` keeps only `release_gate_id`; release version/environment/state live on `ReleaseGate`. |
| Secret env values could leak through agent profiles and heartbeats | `EnvVar` now supports `secret_ref`/`redacted`; secret values must be empty in read/list/heartbeat responses. |
| Start queue permit existed only in docs | Added `AcquireStartPermit` and `ReleaseStartPermit`. |
| MCP subscriptions lacked a cancellation path | Added `CancelMcpResourceSubscription`. |
| Handoff and release gate needed direct lookup/response actions | Added `RespondRoleHandoff` and `GetReleaseGate`. |
| Memory writes could clobber concurrent updates | Added `expected_version` and structured rejection on `UpsertAgentMemory`. |
| Sender identity was represented twice | `Actor` is now the canonical sender shape for message requests and message records. |
| Reminder scheduling had overlapping fields | CLI compatibility time fields are encoded as `oneof` schedule inputs. |
| Task graph updates could clobber concurrent topology edits | Added `expected_graph_version` CAS to `UpdateTaskGraph`. |
| Server cursor validity needed a stable identity | Added `server_id` to `GetServerInfoResponse`. |
| Claim lease renewal failure needed structured handling | Added `rejection_reason` to `RenewTaskClaimLeaseResponse`. |
| Task graph edge updates lost direction/kind | `UpdateTaskGraph` now uses `TaskEdge` for add/remove operations. |
| Activity stream replay lacked ack symmetry | Added `AcknowledgeActivityEvents`; activity subscriptions use cursor-only resume. |
| Run leases had no renewal path | Added `RenewRunLease`. |
| Bare task references were not resolvable | Added `GetTask`. |
| Reminder records had duplicate time fields | `next_run_unix`/`last_run_unix` are canonical; duplicate fire/fired fields are reserved. |
| Final gate residuals | Added `UpdateTask`, task list state/column filters, server stream `idempotency_key`, `duplicate_of` task edge kind, run/run-step activity payloads, and `ReleaseTaskResponse.release_gate`. |

## Pre-Freeze Enum Normalization

Before daemon/bridge implementation, fixed lifecycle and status strings were
converted to protobuf enums. This intentionally breaks the pre-release wire
shape for those fields because `string` and enum values use different protobuf
wire types; no released daemon contract depended on the older shape.

Closed sets now use `*_UNSPECIFIED = 0` and comments on every enum value. The
major normalized groups are:

- task lifecycle, claim policy, claim mode, conflict behavior, edge kind, and
  task source;
- run lifecycle, run-step kind/status, computer status, and MCP subscription
  status;
- agent presence, activity state, health, status severity, role assignment, and
  control operation state/action;
- coordination record kind, work-plan/progress/verification/release/handoff and
  negotiation states;
- reminder lifecycle, schedule kind, event type, and actor type;
- endpoint auth mode, outbound message policy, outbound delivery status;
- channel visibility, channel member roles, and message search sort;
- actor kind, permission scope, memory content format;
- server and collaboration event routing hints.

Open integration points remain strings and carry canonical-value comments in
the proto files. Runtime kind/model/provider, endpoint kind/provider, message
role, capability/permission names, board column projection, and activity
taxonomy are intentionally open so new runtimes, providers, transports, and
activity names do not require another enum migration.

## Collaboration Semantics Check

The current team behavior is not only chat. It includes structured planning,
ownership, conflict handling, progress reporting, review, verification,
handoff, and release decisions. The protocol now has first-class objects for
those concepts:

| Behavior | Protocol object |
| --- | --- |
| Plan with files and acceptance criteria | `WorkPlan`, `WorkPlanItem` |
| Short progress update without losing machine readability | `ProgressUpdate` |
| Task claim, reviewer claim, silent conflict | `Task.claim_policy`, `ClaimCollaborationTaskRequest`, `ClaimCollaborationTaskResponse` |
| Role assignment and handoff between agents | `AgentRoleAssignment`, `RoleHandoff` |
| Verification evidence for review | `VerificationResult`, `AcceptanceEvidence` |
| Release gate before tag/deploy | `ReleaseGate`, `ReleaseTask` |
| Scope/deadline negotiation | `ScopeNegotiation`, `DeadlineNegotiation`, `CounterProposeNegotiation` |
| Memory as durable context rather than only chat history | `MemoryRecord`, `ListAgentMemory`, `UpsertAgentMemory` |
| Link structured records back into chat/event streams | `CollaborationMessage.coordination_record_id`, `ActivityRecord.coordination_record_id`, `CollaborationEvent.coordination_record_id` |

## Remaining Design Boundaries

The protocol intentionally leaves only extensibility points string-based so the
system can evolve without a breaking enum migration:

- runtime kind, provider, and model;
- interaction endpoint kind and provider;
- message role for runtime/provider-specific conversation roles;
- board column projection, which is a view concern distinct from `Task.state`;
- activity taxonomy, capability names, and permission names.

Implementation must validate closed enum transitions at the server boundary and
must preserve unknown/open string values where the proto comments mark them as
extension points.

## Review Checklist

Before daemon/Web implementation resumes, reviewers should confirm:

- the split files keep a clear ownership boundary;
- old message field numbers are preserved;
- new fields are additive and do not rename previous RPCs;
- claim conflict behavior can be represented without sending noisy chat;
- a task can move through plan -> execution -> review -> release gate -> done;
- an agent can hand work to another agent with enough context to continue;
- memory records can point to local `MEMORY.md`/`notes/` paths or server-owned
  records without forcing one storage design;
- non-Web endpoints can create messages/tasks through the same target model.
