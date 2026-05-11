# Multica Frontend Absorption Plan

Status: task #106 initial plan for multi-agent review

This plan translates the mature product/frontend patterns in
`/home/czyt/code/go/references/multica` into a staged Nekode development plan.
It is intentionally a planning document only: it does not require migrating
Nekode to multica's framework stack or changing task #92, #98, or #104 code in
this pass.

## Review Model

Task #106 should be reviewed from four angles before it becomes the frontend
roadmap baseline:

| Reviewer | Scope |
| --- | --- |
| @小吱吱 | Main plan, #92 frontend integration shape, task/board development phases |
| @欧朋扣得 | Frontend architecture, state ownership, SSE/query invalidation, over-design check |
| @小皮丘 | Backend/protocol/API support boundary and follow-up gaps |
| @小螃蟹 | Product IA, board/task interaction, visual hierarchy, exception-state UX |

The plan should only move to done after these reviews either clear it or record
explicit follow-up tasks.

## Multica Patterns Worth Absorbing

### Product Surfaces

Multica is more mature than current Nekode in task/product depth. The useful
surfaces are:

- Task/issue list and board views with shared filter/sort/view state.
- Task detail page with editable title, description, properties, parent/child
  context, activity timeline, comments, reactions, subscribers, attachments, and
  execution logs.
- Agent and runtime pages that make availability, recent work, health, model,
  runtime, skills, and configuration visible.
- Chat/inbox surfaces where realtime task messages are append-only streams while
  final conversation/task state is still read from the database.
- Workspace-scoped navigation and query keys so switching context does not mix
  server state.

Nekode should absorb the interaction model and state boundaries first, not the
full product breadth in one milestone.

### State Ownership

Multica's most important frontend rule is the separation of state authority:

- TanStack Query owns server state.
- Zustand owns client-only UI state such as view mode, filters, sort, collapsed
  columns, card density, draft text, and panel open/closed state.
- WebSocket/SSE events are cache invalidation or append-only stream hints.
- Server data is not mirrored into a UI store as a second source of truth.

Nekode should adopt this rule directly. The exact library can be decided in an
implementation task, but the architecture should be query-cache first. If the
team wants multica parity, TanStack Query plus a small client-state store is the
clearest target.

### Realtime Handling

Multica uses generic event-prefix invalidation for broad changes and specific
handlers for safe cache patches. Nekode's #98/#104 event model maps well:

- `server_id` and protocol version define whether a persisted cursor/cache key
  is valid.
- `sequence` is the explicit browser resume position.
- `operation` gives a conservative cache action hint.
- `EventScope` identifies the fanout/projection family to invalidate, such as
  workspace, target, thread, task, run, agent, computer, user, endpoint, daemon,
  or custom. Message and activity are event kinds or payload families, not
  scopes.
- Append-only message/activity payloads can be patched with sequence dedupe when
  they arrive under a target/thread/task/run/agent scope.
- Complex denormalized views should invalidate/refetch by default.

This keeps SSE fast without making the browser an authority for task state.

Current implementation note: task #98/#104 defines the event shape and durable
replay path, but event producers do not yet cover every HTTP and daemon
message/task mutation. Phase 1 can wire SSE resume/cursor and activity replay,
but broad board/message realtime invalidation requires the backend producer
follow-up listed below.

Conservative initial mapping (default: invalidate/refetch; exceptions:
append-only payloads only):

| event kind / payload | EventScope | Frontend action |
| --- | --- | --- |
| message appended | target, thread | Append to the relevant message stream only when sequence dedupe is available; otherwise invalidate/refetch |
| activity appended | target, thread, task, run, agent | Append to the relevant activity stream only when sequence dedupe is available; otherwise invalidate/refetch |
| task created/updated/state_changed/claimed/released/deleted | task, target, workspace | Invalidate and refetch task list, board, and detail queries |
| agent_control, agent status | agent, computer, daemon | Invalidate and refetch agent, daemon, runtime, or run projections |
| run | run, computer, daemon | Invalidate and refetch run and step projections |
| coordination, memory | workspace, user | Invalidate and refetch coordination or memory queries |
| unknown kind or scope | any | Prefer broad invalidate/refetch over local patching |

### Task Board

Multica's board patterns to absorb:

- Board and list share the same query data.
- Columns are derived from task status and filter state.
- Status buckets can paginate independently.
- Drag/drop keeps temporary local column state while dragging, then commits a
  status/position mutation.
- Hidden columns and card property toggles are UI preferences, not server facts.
- Batch operations are useful after selection and query invalidation are stable.

Nekode should start with the six-state board already supported by #104 and avoid
drag/drop ordering until the API has a stable position or reorder contract.

## Current Nekode Baseline

### Already Available

- React/Vite/TypeScript frontend skeleton under task #92.
- HTTP auth, messages, tasks, interaction endpoints, protocol info.
- Task board skeleton with current channel/target filtering.
- HTTP bridge and SSE surface from task #98.
- Durable event log, durable idempotency, task claim CAS, bootstrap guard.
- Cache interface with Badger default and Redis provider boundary, restricted to
  rebuildable projections.
- Six task states after task #104: `todo`, `in_progress`, `blocked`,
  `in_review`, `done`, `canceled`; `cancelled` is only a compatibility alias.
- SSE explicit resume via `cursor` or `sequence`, with token query support for
  browser `EventSource`.

### Not Yet Equivalent To Multica

Nekode does not yet have multica-equivalent product capability. The main gaps:

- No real query-cache layer or frontend server-state architecture yet.
- Task #92 frontend still needs real API/SSE integration and DTO normalization.
- No task detail page with description, properties, comments, timeline, or
  activity history.
- No mature board controls: filters, sorting, list/board switch, quick create,
  selection, batch actions, hidden columns, per-column pagination.
- No stable frontend drag/drop reorder contract yet.
- Agent/runtime pages are minimal compared with multica's availability, health,
  recent-work, runtime, model, and skill management surfaces.
- Workspace/server scoping is in protocol shape, but frontend query keys and
  navigation are not mature.

## Capability Gap Matrix

| Area | Multica Pattern | Nekode State | Plan |
| --- | --- | --- | --- |
| Server-state cache | Workspace-scoped TanStack Query keys | Current #92 uses local React state | Add query-cache architecture after API DTO boundary is stable |
| UI state | Zustand for filters, sort, drafts, panels | Local component state in one large shell | Extract local UI store or equivalent client-only state layer |
| Board states | Multiple statuses with board-specific visible columns | Six backend states available after #104 | Show six columns in order: todo, in_progress, blocked, in_review, done, canceled |
| Board controls | Filters, sort, list/board, hidden columns | Not present | Add after query-cache layer |
| Ordering | Drag/drop position mutation | No stable frontend reorder contract | Backend follow-up before drag/drop |
| Task detail | Properties, comments, timeline, reactions, attachments | Not present | Build detail/timeline incrementally after #92 SSE |
| Realtime | WS invalidates/patches query cache | SSE event shape available | Implement conservative invalidate/refetch; patch append-only streams only |
| Cursor validity | Workspace identity protects cache/cursor | `server_id` exists in daemon info/event cursor | Store cursor with `server_id`; drop cursor on mismatch |
| Agents | Availability, ownership, runtime/model/skills, recent activity | Agent status bridge exists | Add list/detail panels after task board foundations |
| Runtimes | Health filters, local daemon card, remote runtime connection | Daemon bridge info/status exists | Add health/status projections after backend exposes stable DTOs |
| Permissions | UI gates actions and reports denied operations | Basic auth exists | Add permission model UX when backend defines action-level errors |
| Self-hosting | Env/docs matrix and setup flows | API/cache docs exist | Expand docs after product surfaces stabilize |

## Development Plan

### Phase 0: Stabilize Backend/Protocol Prerequisites

Owner: task #98/#104 backend owners, with frontend review.

Required before real frontend parity work:

- Keep task #104 six-state support green across storage, HTTP, daemon connect-rpc, board
  projection, docs, and tests.
- Treat `canceled` as canonical in frontend types; `cancelled` remains API
  compatibility only.
- Keep `/api/server-events` resume semantics explicit: frontend reconnects with
  `sequence` or cursor, not browser `Last-Event-ID`.
- Keep cache and event-log authority boundaries documented: frontend cache is a
  projection, not a fact store.

Acceptance:

- `GET /api/tasks`, `POST /api/tasks`, `PATCH /api/tasks/{id}`, daemon
  `UpdateTask`, and daemon `ListTaskBoard` all support six states.
- SSE events include enough `sequence`, `operation`, `scope`, `server_id`, and
  protocol information for frontend invalidation.

### Phase 1: Real #92 API/SSE Integration

Goal: replace placeholder frontend boundaries with a reliable typed bridge.

Work:

- Expand `TaskState` to `todo | in_progress | blocked | in_review | done |
  canceled`.
- Render six board columns in this exact order: `todo -> in_progress ->
  blocked -> in_review -> done -> canceled`. Board column order is fixed and
  not user-reorderable.
- Normalize bridge DTOs in the API client layer, including snake_case/camelCase
  and protobuf JSON enum numbers.
- Extend `subscribeServerEvents` to pass `access_token` for browser
  `EventSource`.
- Persist the latest `data.sequence` and reconnect with `sequence`.
- Fetch daemon info and bind persisted cursor state to `server_id` and protocol
  version; if either changes, drop the cursor and replay from `sequence=0`.
- Start with conservative event handling: invalidate/refetch task/message/agent
  queries by event kind, `operation`, and `EventScope`; only patch append-only
  message/activity payloads when sequence dedupe is available.

Acceptance:

- Browser reload/reconnect resumes without losing events.
- Server identity change invalidates old cursor state.
- A task moved into `blocked` or `canceled` appears in the correct column.
- Components do not know SSE cursor/token details; those stay in API/realtime
  modules.

### Phase 2: Frontend State Architecture

Goal: make the frontend able to grow without duplicating server state.

Work:

- Introduce a query-cache layer for server data. TanStack Query is the direct
  multica-aligned option; if a lighter custom layer is chosen, it must still
  provide scoped keys, invalidation, optimistic updates, and stale/error states.
- Define query keys with `serverId`, `target`, and later `workspaceId`.
- Move UI-only state into a small client store or isolated hooks: active view,
  filters, sorting, column visibility, selected task IDs, composer drafts, and
  panel state.
- Split the current one-file shell into feature modules:
  - `api/`
  - `realtime/`
  - `tasks/`
  - `messages/`
  - `daemon/`
  - `agents/`
  - `layout/`

Acceptance:

- No server task/message/agent data is mirrored into a UI store.
- Query keys for server-scoped resources include `serverId` and protocol
  version; when either changes, related queries, cursors, and projections are
  invalidated.
- `serverId` must be included in all query keys for server-scoped resources
  (tasks, messages, agents, runs, etc.); when `serverId` or protocol version
  changes, all related queries, cursors, and cached projections are dropped and
  replayed from `sequence=0`.
- Query invalidation paths are testable without rendering the whole app.
- View filters/sort survive normal navigation without corrupting server cache.

### Phase 3: Task Board Product Maturity

Goal: close the highest-value gap between current Nekode and multica task UI.

Work:

- Add board/list switch backed by the same task query data.
- Add status, assignee, target/channel, creator, and text filters.
- Add sorting by created time, updated time, status, assignee, and title.
- Add quick create per column with the column's state prefilled.
- Add editable card actions for status and assignee.
- Add selection and batch actions only after single-item mutations are stable.
- Add hidden/collapsed column preferences as UI state.
- Add per-column count and empty states.
- Add mobile board interaction: segmented control or horizontal scroll to switch
  between the six columns, or use status filter as an alternative to column view.
- Add navigation active state indication: current selected target/agent should
  have clear visual feedback (background, left border, bold, etc.) in the left
  sidebar; breadcrumb or title should display current context.
- Add quick search (Cmd+K or Cmd+F) for rapid task discovery.

Deferred until backend support:

- Drag/drop reorder and manual position.
- Cross-target board aggregation if target/workspace semantics are not yet final.

Acceptance:

- Board and list stay consistent after create/update/delete and SSE events.
- Filters and sort do not change server facts.
- `blocked` and `canceled` have distinct visual treatment and are always
  discoverable.
- `blocked` and `canceled` remain quickly scannable in dense board/list views
  without relying on color alone; status must be communicated by text, icon, or
  shape in addition to color.
- Mobile board interaction allows access to all six columns without horizontal
  overflow or layout collapse.

### Phase 4: Task Detail, Timeline, and Collaboration

Goal: move from board cards to a task work surface.

Work:

- Add task detail route/panel.
- Show summary, description, state, assignee, target, creator, timestamps, and
  daemon/runtime metadata when available.
- Add activity timeline from durable events.
- Add comments/messages associated with a task or target thread.
- Add links from board card to detail and from event/activity items back to task.
- Add append-only timeline updates with sequence dedupe; invalidate/refetch on
  complex updates.
- Add blocked reason display and edit capability; blocked reason is required when
  transitioning a task to blocked state.
- Add optimistic update conflict handling: when a user edits a task locally and
  receives an SSE update that changes the same field, show a conflict indicator
  and offer "refresh" or "keep local" options instead of silently overwriting.

Backend follow-up candidates:

- Task description if not already first-class.
- Comment/thread relation to task.
- Attachment/reaction/subscriber/pin APIs if these become product requirements.
- Permission errors with stable machine-readable codes.

Acceptance:

- A user can inspect why a task is blocked, who changed it, and what event
  sequence produced the current state.
- Reconnect/replay does not duplicate timeline entries.
- Blocked reason is always visible and editable on blocked tasks.

### Phase 5: Agents, Daemon, and Runtime Surfaces

Goal: expose the agent-native parts that make Nekode distinct.

Work:

- Add agent list with status, current task, last heartbeat, recent activity, and
  availability.
- Add daemon/runtime health view with local bridge identity, cache driver, RPC
  URL, connected computers, leases, and heartbeat state.
- Add run detail panels: attempt, max attempts, parent run, failure reason, last
  heartbeat, step log, and lease status.
- Add offline state handling: when a daemon or agent is offline, mark related
  tasks with a disabled or warning badge so users can distinguish between
  "blocked by external dependency" and "blocked by offline runtime".
- Use SSE scope to invalidate agent/runtime/run queries.
- Keep command/control actions behind explicit affordances and permission
  checks.

Acceptance:

- User can tell whether an agent is available, busy, offline, blocked, or
  failing due to runtime/lease issues.
- Runtime failures surface as actionable UI states, not just raw logs.
- Offline daemons/agents are clearly marked in the board and agent list.

### Phase 6: Product Polish and Self-Hosting Readiness

Goal: make the product understandable and operable beyond the development team.

Work:

- Add empty/loading/error/offline states for every core surface.
- Add keyboard navigation for board/list/detail.
- Add search across tasks/messages/agents after backend API exists.
- Add settings pages for cache driver visibility, daemon bridge info, tokens,
  endpoint setup, and self-hosting diagnostics.
- Add permission UX: disabled buttons with tooltips explaining why an action is
  unavailable, or hide actions entirely based on product decision.
- Expand self-hosting docs with env matrix, local daemon setup, Redis/Badger
  cache selection, and recovery steps.

Acceptance:

- A fresh self-hosted user can bootstrap, inspect daemon status, create a task,
  watch it update, and recover from reconnect without reading source code.
- Permission-denied operations are clearly communicated to the user.

## Dependency Decisions To Make Later

Do not decide these inside task #106; record them as implementation choices for
future tasks:

- Whether to add TanStack Query and Zustand directly.
- Whether to add React Router or keep view state internal for now.
- Whether to add dnd-kit, and only after backend ordering exists.
- Whether to introduce a shared UI component library.
- Whether to create a workspace abstraction now or keep target/server scoping
  until multi-workspace semantics exist.

## Frontend Style Prompt Input

If a separate Claude pass is used to generate a reusable frontend style prompt,
give it these requirements instead of a vague request for a "nice UI":

- Product identity: Nekode is an AI-native collaboration and engineering
  control console. It is not a marketing site, landing page, or generic SaaS
  admin template. The first viewport should be a working application surface.
- Layout: left navigation for workspace/target/agent/runtime context, central
  task board/list/message/activity work area, optional right-side task detail or
  agent/runtime inspector.
- References: absorb Linear and multica's calm, dense, professional workflow
  feel, but keep Nekode's daemon/agent collaboration identity.
- Task board: design six states in this exact order: `todo`, `in_progress`,
  `blocked`, `in_review`, `done`, `canceled`. Each column needs count, empty
  state, loading state, and distinct but restrained status treatment.
- State colors: neutral for todo, blue or cyan for in progress, amber/orange for
  blocked, purple or violet for review, green for done, muted red/gray for
  canceled. Status must not be communicated by color alone.
- State language: user-facing canonical copy is `canceled`, not `cancelled`.
  `cancelled` is only an HTTP/storage input compatibility alias.
- Visual direction: operational cockpit, high scanability, long-session comfort,
  restrained borders, small radius not exceeding 8px for panels/cards, semantic
  color only where it helps interpretation.
- Avoid: marketing hero sections, large decorative gradients, orbs, AI SaaS
  template visuals, oversized typography inside tool panels, nested cards, and
  palettes dominated by a single purple/blue, cream, or dark-slate theme.
- Components: buttons, inputs, menus, tabs, segmented controls, filter chips,
  status badges, skeletons, empty states, retry/error panels, toast/inline
  feedback, task cards, detail panels, agent/runtime rows, and timeline items.
- Icons: use lucide-style icons. Prefer icons or icon+short-label controls for
  common actions.
- Interaction: board filters/sort/quick create/state change, task detail
  timeline/comments/activity, agent/runtime health, and subtle realtime update
  feedback. SSE updates should feel informative, not noisy, and must be framed
  as lightweight status hints rather than direct authoritative task movement.
- State architecture constraints: server state belongs to a query-cache layer;
  UI store only owns filters, selected IDs, drafts, panel state, and density
  preferences. DTO/proto JSON mapping stays in the API client layer.
- Realtime constraints: `data.sequence` drives resume, EventSource token is
  hidden inside the API client, `server_id` changes invalidate cursor/projection
  state, and events default to invalidate/refetch except append-only streams.
  Final board movement comes from refetched/query-cached server DTO state, not
  from components mutating state directly from raw SSE events.
- Responsive rules: desktop shows the multi-column board with horizontal
  overflow if needed, tablet preserves board scanning, mobile becomes a
  single-column/task-list view with status filters.
- Accessibility: keyboard reachability, visible focus states, no text overflow,
  no incoherent overlap, status text/icons in addition to color, and usable
  contrast in light and dark themes.
- Theme foundation: `task #198` adds the first Web light/dark/system theme
  switch with local persistence and first-paint `data-theme` initialization.
  Future visual polish should reuse the existing CSS token layer instead of
  adding raw one-off colors.
- Desired Claude output: a reusable frontend style prompt with design principles,
  tokens for color/type/spacing/radius/status, layout rules, component rules,
  animation limits, responsive rules, forbidden patterns, and an acceptance
  checklist.

## Non-Goals

- Do not migrate Nekode to Next.js, Turborepo, or multica's monorepo layout as
  part of this plan.
- Do not make Badger, Redis, or browser cache authoritative for task state,
  event sequence, idempotency, leases, sessions, tokens, secrets, or config.
- Do not implement the whole multica issue product in Milestone 3.
- Do not drag #92 frontend implementation into task #106; use this document to
  create or refine follow-up implementation tasks.

## Immediate Follow-Up Tasks

Recommended after this plan is reviewed:

1. **Backend event producer slice**: HTTP and daemon message/task create/update/claim operations must append durable `collaboration_events` with `kind`, `operation`, `scope`, and `aggregate_id` so that Phase 1 frontend can assume board/message realtime invalidation is covered. Currently only `LogActivity` appends events; message and task mutations write storage but do not emit collaboration events.

2. Task #92 integration slice: real API/SSE boundary, six task states, DTO
   normalization, token query, sequence resume, server_id cursor invalidation.

3. Frontend state architecture slice: query keys, query-cache layer, UI-only
   state split, feature module extraction.

4. Board maturity slice: filters, sort, list/board switch, quick create, column
   counts, visual treatment for `blocked` and `canceled`.

5. Backend protocol follow-up: stable task ordering/reorder contract before
   drag/drop.

6. Task detail follow-up: task description, durable activity timeline, comments
   or linked message thread, and permission/error DTOs.
