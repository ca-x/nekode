# Frontend Redesign Plan (Slock-inspired, brand colors preserved)

Owner: czyt · Started: 2026-05-09 · Status: M1 in progress

## 1. Goals

Rebuild the web console around four object-first primary nav items (Messages,
Tasks, People, Computers) with a three-pane shell (primary rail / context rail
/ content + optional detail panel). The visual system reuses the current
blue-grey brand palette in light and dark; only structure, spacing, and
interaction patterns are replaced. Slock.ai is the reference model for the
information architecture of People, Computers, and the Agent detail page.

Non-goals:

- No brand-color change. Existing palette stays.
- No feature additions that lack backend support (Skills tab, Workspace tab,
  Report Issue button). Removed or shown as "not implemented yet" placeholder.
- No strict dark mode. Default follows the system.
- No forced mobile rewrite. Mobile is a tier we must support, not the primary
  design surface.

## 2. Information architecture

### Primary rail (desktop) / Bottom tab bar (mobile)

| Slot | Label | Scope |
|---|---|---|
| 1 | Messages | Conversations (channels, DMs, threads) |
| 2 | Tasks | Kanban + list, reminders are nested inside messages |
| 3 | People | Humans (flat) + Agents grouped by computer |
| 4 | Computers | Computer list + detail (with agents) |
| footer | Settings | Gear icon on the primary rail footer |
| footer | Alerts | Pill list above the gear when alerts exist |

### Context rail contents per primary

- **Messages** — Channels (unread badge), DMs (humans and agents both), Threads
  expand in right panel.
- **Tasks** — Views (All / Mine / By status), filters preserved across nav.
- **People** — `PEOPLE` group (flat humans), `AGENTS` group (collapsible
  sub-sections per computer: `▶ lightos (6)`, `▶ my-computer (2)`).
- **Computers** — Flat computer list with status dot + daemon version + `+`.

### Right detail panel (opt-in)

Hosts one of: Thread, Agent detail, Channel settings, Computer detail (on
tablet widths). Closeable. On mobile it becomes a full-screen sheet.

## 3. Screen specifications

### 3.1 Computer detail

**Header** — Icon + hostname + online dot + archtype subtitle; inline NAME
editor with pencil toggle.

**INFO section**
- OS: `linux x64` (data: `daemonInventory[].platform + arch`).
- Daemon Version: `v0.46.1`. If `version < targetDaemonVersion` render
  `Update available` in warning color linking to release notes. The target
  version is emitted by a server endpoint (e.g. `GET /api/system/info`
  returning `{ targetDaemonVersion }`) so the client picks up operator-side
  upgrades automatically.
- Detected Runtimes: chip row, **installed chips first**, each group in the
  order defined by `runtimeCatalog`. Installed chips use primary-soft fill;
  missing chips use muted outline + `(not installed)` suffix. Source: merge
  `daemonInventory[i].runtimes` against `runtimeCatalog`.
- Created: formatted localized date.

**CONNECT COMMAND** — Monospace block with the install curl (regenerated via
the existing enrollment install-code flow). Copy button top-right. Helper
text: "Keep this process running — it maintains the connection between your
computer and nekode." Reuses the freshly-built TTL-based install code so
retrying on flaky networks works.

**AGENTS ON THIS COMPUTER (N)**
- Header actions: `Start All` (enabled only when any agent is paused/stopped)
  and `Create`.
- Row: avatar + agent name + runtime chip + chevron. Whole row is clickable.

**ACTIONS** — Danger zone. `Delete Computer` red button, **disabled** with
helper text "All agents must be deleted first" when agent count > 0.

### 3.2 Agent detail (five tabs)

Accessible from the People rail (click an agent) or from a Computer detail
row. Same screen, same URL hash.

| Tab | Source | Notes |
|---|---|---|
| PROFILE | Agent record | Avatar + display name + description (inline edit), INFO (Computer link, Created, Creator or "No creator assigned"), RUNTIME CONFIGURATION (Runtime + Model, editable), ENVIRONMENT VARIABLES (table with add/remove), CREATED AGENTS (count), offline banner with `Retry` when daemon offline, ACTIONS row (Start / Restart / Delete). **No Report Issue** (nekode has no issue system). |
| AGENT DMS | `dm:{agentId}` message stream | Reuses MessagesPanel in embedded mode. |
| REMINDERS | Reminders stream filtered to this agent, **preferring `reminder.targetAgentId`** when present and falling back to target-string parsing only if that field is absent. No server-side filter needed — agents schedule with their own identity. | Live-updates via the existing realtime stream. Empty state: "This agent hasn't scheduled anything. Reminders appear here in real time as soon as the agent schedules them." |
| WORKSPACE | — | Tab present but shows empty state until the backend file-browser API lands. No faux tree. |
| ACTIVITY | Activity stream filtered by agent | Timeline format. |

### 3.3 People rail

Upper group `PEOPLE` lists humans from `useList(users)`. Lower group `AGENTS`
groups by `daemonInventory[].computerId`; each group header shows the computer
name + total running. Agents in the list show avatar + name + runtime chip +
status dot (running/idle/error). Clicking opens Agent detail.

### 3.4 Computers rail

Flat list; each row = pixel icon + hostname + `daemon v…` + status dot.
Selected row gets `--primary-soft` background + 2px primary left bar. `+` in
section header opens the Connect Computer modal.

### 3.5 Connect Computer modal

- Monospace command block: `sudo bash -c "$(curl -fsSL <install_url>)"`. Install
  URL comes from the freshly-fixed `absoluteURL` / `GRPCAdvertiseAddr` path so
  it resolves to the public host, not `localhost`.
- Copy icon top-right.
- Status pill below the block, polling the enrollment status endpoint:
  - `Waiting for computer to connect…` (warning tone, pulsating dot)
  - `Connected` (success tone, dot solid)
- Footer buttons: `Cancel` / `Done` (Done disabled until status == connected).

## 4. Visual system

### Tokens (new `tokens.css`, already added)

- **Spacing** `--space-{0..16}` at 4pt rhythm.
- **Radius** `--radius-{xs..xl,full}`.
- **Typography** `--font-size-{11..28}` + `--line-height-{tight,snug,normal,relaxed}`.
- **Motion** `--duration-{fast,base,slow}` + `--ease-{out,in,in-out}`; reduced-motion respected.
- **Layout metrics** `--rail-primary-width`, `--rail-context-width`,
  `--rail-detail-width`, `--app-bar-height`, `--mobile-tab-bar-height`.
- **Elevation** `--elevation-{1..4}`.
- **Semantic** `--agent-accent*`, `--alert-*` mapping onto the existing palette.

Components must reference these tokens, not raw hex or px literals, unless
the value is an intentional pixel-perfect constant (e.g. `1px` borders).

### Breakpoints

`480 / 768 / 1024 / 1440`. Three-pane layout collapses at `1024` (context rail
becomes a slide-over), primary rail becomes bottom tab bar at `768`.

### Focus ring

`2px solid var(--focus-ring-color)` with `2px` offset on every shell-level
focusable surface.

## 5. Interaction rules

- 44×44pt minimum touch target (48 on mobile); icon-only buttons expand via
  padding or `hitSlop`-equivalent (invisible padding ring).
- Press feedback via opacity/background only. No transform scale that shifts
  layout bounds.
- Async buttons self-disable and show a spinner adjacent to their label.
- Keyboard tab order matches visual order; all modals trap focus and restore
  it on close.
- Destructive actions live in a visually separate row and require disabled
  state when preconditions aren't met (e.g. Delete Computer).
- Errors render inline under the related field, not in a global toast, unless
  the error is not attributable to a single field.
- Toasts auto-dismiss in 3–5s, use `aria-live="polite"`, never steal focus.
- Modals close on Escape; dirty forms prompt before dismiss.

## 6. Accessibility

- All meaningful icons have `aria-label` or adjacent text.
- Form inputs have `<label for>`.
- Color is never the only indicator — status dots pair with status text,
  installed runtime chips pair with `(not installed)` suffix.
- Contrast 4.5:1 body / 3:1 large text in both themes. Dark mode is an
  independently-tuned palette, not an inverse.
- `prefers-reduced-motion` short-circuits transitions via tokens.

## 6.5 Locked decisions (from design review)

- **Brand palette**: unchanged. Reuse the existing blue-grey tokens defined
  in `styles.css`.
- **Default theme**: follow system (no forced dark mode).
- **Avatars**: keep the current geometric color block + initial letter
  component. No pixel-art set, no `avatarUrl` field for this pass.
- **People rail**: `PEOPLE` group for humans, `AGENTS` group grouped by
  computer. Agent management (pause/stop/delete) reachable from both the
  People rail and the Computers rail, but **agent creation lives only inside
  Computer detail** because it requires a target computer.
- **Workspace tab**: kept in the Agent detail IA with an empty state until
  the backend lands. Prevents churn in the tab set later.
- **Detected Runtimes order**: installed chips first, each group in
  `runtimeCatalog` order.
- **Reminders filter source**: prefer `reminder.targetAgentId`; fall back to
  parsing `reminder.target` only if the field is absent.
- **Start All**: parallel, unthrottled on the client; per-agent failures
  show as inline chips on the agent row.
- **targetDaemonVersion**: emitted by the server (`GET /api/system/info`),
  not hardcoded client-side.
- **Report Issue button**: removed from the Agent ACTIONS row (nekode has
  no issue system).

## 7. Migration strategy (incremental)

The redesign lives alongside the current UI until a milestone is complete and
verified. Old panels keep rendering under their current classes; new panels
opt into `.layout-shell`. Each milestone ships as a small diff that builds
cleanly on its own.

### M1 — Cleanup + layout primitives (this milestone)

- [x] Add `tokens.css` with spacing, radius, typography, motion, layout,
  elevation, semantic tokens.
- [x] Wire `tokens.css` into `main.tsx`.
- [x] Remove Skills nav entry, Skills view, `skillItems` constant.
- [ ] Remove Slock/Multica chips from runtime presets in the legacy
  SkillsPanel. Drop SkillsPanel entirely.
- [ ] Prune any now-unused imports.
- [ ] `tsc --noEmit` and manual smoke check.

### M2 — Primary + context rails + mobile tab bar + hash routing

- Introduce `<Shell>` component wrapping `.layout-shell`.
- Four-slot primary rail on desktop; bottom tab bar on mobile.
- Context rail swaps per primary selection.
- Hash routing (`#/messages`, `#/tasks`, `#/people/agent/<id>`, etc.) so deep
  links work and back-button preserves state.
- Settings gear moves to the primary rail footer; the `settings` view is
  reachable via hash.
- Old nav removed once parity confirmed.

### M2.5 — Sidebar footer alerts

- Alerts list above the gear:
  - Computer offline warning (when any inventory entry heartbeat older than
    the threshold).
  - Daemon version out of date (when any daemon version < target).
- Each alert is a clickable pill that links to the offending detail page.
- Alerts auto-dismiss when the underlying condition resolves; no manual
  dismiss for warnings that matter for operation.

### M3a — Computers view

- ComputersListPanel for the context rail.
- ComputerDetailPanel with header / INFO / CONNECT COMMAND / AGENTS / ACTIONS.
- NewComputerModal reusing the existing enrollment flow but presented as a
  focused modal with polling pill.
- Wire `Delete Computer` guard: disabled unless agent count is zero.

### M3b — Agent detail

- AgentDetailPanel with PROFILE / AGENT DMS / REMINDERS / WORKSPACE / ACTIVITY.
- WORKSPACE tab is a static "not implemented yet" empty state.
- Wire `Start` / `Restart` / `Delete`.
- Remove the legacy DaemonPanel after the new screens achieve feature parity.

### M4 — Endpoints + Settings restructure

- Endpoints: context rail list + detail panel, QR visible only in ilink mode
  (already patched in an earlier pass, keep the logic).
- Settings split into tabs: Account / Users & Perms / IM Providers /
  Runtimes / Notification Routing / System.

### M5 — Mobile polish

- Convert every `min-width: 760px` table to card rows below `768`.
- Sticky app bar with back affordance and current path.
- Input height ≥48px on mobile; forms single column.
- Right detail panel becomes full-screen sheet with drag-to-dismiss.

### M6 — Wrap-up

- Top-of-shell banner when daemon is disconnected, with a `How to fix` modal.
- `/` command skeleton for the composer (out of scope unless backend lands).
- Final accessibility pass, contrast audit, keyboard pass.

## 8. Open questions

1. `targetDaemonVersion` — **resolved**: emitted by a server endpoint
   (likely `GET /api/system/info`), consumed by the console on load and on
   focus.
2. Workspace tab backend — deferred past M6. Tab remains visible with empty
   state so the IA stays stable when the backend lands.
3. Reminders per-agent filter — **resolved**: agents schedule reminders with
   their own identity; the console filters client-side, preferring the
   structured `reminder.targetAgentId` field and falling back to parsing the
   target string only when the field is absent. Empty state copy: *"This
   agent hasn't scheduled anything. Reminders appear here in real time as
   soon as the agent schedules them."*
4. `Start All` semantics — **resolved**: parallel, no client-side rate limit.
   Requests fail independently; failures surface as per-row error chips on
   the agent list, not a global modal.

## 9. Verification checklist per milestone

Each milestone is done when:

- `tsc --noEmit` passes.
- `go build ./...` passes.
- Relevant tests pass.
- The new screen renders in both light and dark at 1440, 1024, 768, 480.
- Keyboard-only walkthrough reaches every action.
- Contrast ratios checked for any new color usage.
- Removed surfaces (e.g. Skills) no longer reachable from any deep link.
- **Codex review signs off.** After local verification, spawn a codex
  reviewer agent with the milestone diff, the milestone section of this plan,
  and the locked decisions in §6.5 as context. Reviewer checks:
  - diff matches the milestone scope (no scope creep, no leftover dead code)
  - locked decisions in §6.5 are respected
  - accessibility and responsive rules from §5–§6 hold
  - any new backend assumptions (e.g. `GET /api/system/info`,
    `reminder.targetAgentId`) are flagged if missing from the server
  When codex returns blocker-level findings, fix them and re-run codex until
  it signs off. No human review is required; local checks + codex pass is
  the merge gate.
- **After codex signs off, commit and push.** Commit with a
  `frontend: M<n> — <scope>` message summarizing the milestone, push to the
  current branch. This turns each milestone into a reviewable Git checkpoint
  so regressions can be bisected per milestone.
