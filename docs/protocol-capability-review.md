# Protocol Capability Review

Status: review draft
Date: 2026-05-07

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
| Server connection, machine lock, registration, heartbeat | `RegisterComputer`, `HeartbeatComputer`, `Lease`, `ComputerInfo`, and `ComputerInventory`. |
| Runtime discovery and launch queue visibility | `Runtime`, `AgentStatusSnapshot`, `Run`, `RunStep`, and activity records can report queued/running/blocked states. |
| Agent-scoped token/CLI bridge injection | Represented by `RuntimeProfile`, `EnvVar.secret`, `Workspace`, and memory/workspace boundaries. |
| Public join-to-write and permission checks | `Permission`, `ChannelRecord`, `InteractionEndpoint`, and task/message mutation requests carry actor and endpoint ids. |
| Profile update for display name, description, avatar | `UpdateAgentProfileRequest` keeps these additive fields. |
| Reminder snooze/update/log | `SnoozeReminder`, `UpdateReminder`, `GetReminderLog`, `ReminderEvent`, and recurrence fields. |
| Reconnect-preserved online status | `Lease`, heartbeat, and `AgentStatusSnapshot` separate daemon liveness from agent activity. |
| Silent failed claim behavior | `ClaimCollaborationTaskRequest.silent_on_conflict` and response conflict fields make this a machine-visible client choice. |

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
| Release gate before tag/deploy | `ReleaseGate` |
| Scope/deadline negotiation | `ScopeNegotiation`, `DeadlineNegotiation` |
| Memory as durable context rather than only chat history | `MemoryRecord`, `ListAgentMemory`, `UpsertAgentMemory` |
| Link structured records back into chat/event streams | `CollaborationMessage.coordination_record_id`, `ActivityRecord.coordination_record_id`, `CollaborationEvent.coordination_record_id` |

## Remaining Design Boundaries

The protocol intentionally leaves these values string-based so the system can
evolve without a breaking enum migration:

- runtime kind and provider;
- interaction endpoint kind/provider/auth mode;
- task state, board column, claim policy, and conflict behavior;
- agent role names;
- coordination record kind and status;
- memory scope and content format.

Implementation must validate allowed values at the server boundary, but the
wire contract should remain open-ended.

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

