# Slock Daemon 0.46.1 Parity Plan

Status: next milestone planning
Related tasks: task #225
Reference: Slock daemon 0.46.1 release notice from 2026-05-09

This plan records the daemon 0.46.1 behaviors that are worth evaluating for
Nekode. It also confirms that the current IM optimization plan already exists
in the next-milestone IM documents, so this daemon parity work should not block
or duplicate the task #212, task #213, and task #214 IM implementation track.

## IM Plan Check

IM enhancement is already written into the development plan:

- `docs/im-capability-next-milestone-plan.md` covers task #212 interaction
  capability matrix/schema, task #213 capability-driven planner and weak-channel
  fallback policy, and task #214 Telegram rich interactions.
- `docs/stella-plugin-architecture-plan.md` covers task #217 pluginization for
  agent runtimes, IM channels, and daemon-side structured probes.
- `README.md` and `docs/im-channel-integration.md` point to both plans.

Implementation order remains:

1. task #212: add the interaction capability matrix and provider schema.
2. task #213: add the planner and fallback policy.
3. task #214: add Telegram rich interactions through that planner.
4. task #217: proceed as a plugin architecture migration lane, not as a
   prerequisite for the first capability planner.

## Daemon 0.46.1 Delta

### 1. Reply-Target Hints

Slock daemon 0.46.1 delivery prompts include a direct reply-target hint, so an
agent does not have to infer whether to reply to a channel, DM, or thread from
the raw message header.

Nekode relevance:

- High. Recent multi-agent work shows that incorrect or ambiguous reply targets
  create noisy coordination and misplaced status updates.
- Nekode launch prompt snapshots already carry run target and message/thread
  context, but the communication protocol should expose a single computed
  reply target for the active input.

Proposed development task:

- Add a server-computed `reply_target_hint` to launch prompt snapshot content;
  a later protocol pass can add a machine-readable snapshot field or daemon
  delivery event field if needed.
- The hint should be the exact target string to use when responding:
  `#channel`, `#channel:thread`, `dm:@name`, or `dm:@name:thread`.
- Tests should cover channel messages, channel thread replies, DMs, and DM
  threads.

Acceptance:

- The daemon-injected prompt includes a visible reply-target hint for every
  message-triggered run.
- Agent instructions say to use the hint instead of reconstructing targets from
  message text.
- Existing target/thread fields remain for audit and backward compatibility.

Estimated effort: 1 small backend slice plus focused daemon prompt tests.

### 2. Channel Membership System Events

Slock daemon 0.46.1 delivers channel membership changes as system messages in
the affected channel.

Nekode relevance:

- High. Agents need an observable reason when they are added to or removed from
  a channel, especially in long-lived daemon sessions.
- Nekode already has channel membership storage and collaboration events, but
  the membership-change behavior should be explicit at the message/event layer.

Proposed development task:

- Emit a system message or collaboration event when an agent is added to or
  removed from a channel.
- Include actor, affected channel, affected member, and operation
  (`member_added` or `member_removed`).
- Ensure channel event delivery and Web activity views render the event without
  making agents send their own explanatory messages.

Acceptance:

- Adding an agent to a channel produces a visible system record in that channel.
- Removing an agent produces a visible system record before ordinary channel
  delivery stops for that agent.
- Tests cover both add and remove operations plus permission boundaries.

Estimated effort: 1 medium server/Web slice because it touches membership
mutation, event projection, and visible system-message rendering.

### 3. Workspace And Activity Visibility Scoping

Slock daemon 0.46.1 restricts other agents' workspaces and activity logs to
agents that share a channel.

Nekode relevance:

- High for privacy. Nekode stores agents, workspaces, runs, and activity in a
  shared server. Visibility must not drift into global observability for every
  authenticated agent.
- This should be treated as an authorization and projection rule, not only a Web
  filtering rule.

Proposed development task:

- Define a shared-channel visibility predicate for agent workspace, run, and
  activity queries.
- Apply the predicate in HTTP APIs, daemon bridge APIs, and Web projections.
- Keep server admins able to inspect operational state through explicit admin
  paths if the product requires that role.

Acceptance:

- An agent can view another agent workspace/activity only when they share at
  least one channel or when an explicit admin path is used.
- Tests cover shared-channel allow, no-shared-channel deny, and admin access.
- Web does not leak hidden agent workspace/activity through aggregate counters
  or detail drawers.

Estimated effort: 1 medium to large authorization slice. Treat as higher risk
than UI-only filtering because the boundary must be server-enforced.

### 4. Gemini Windows Stdin Launch

Slock daemon 0.46.1 fixes Gemini on Windows by sending the long wake prompt over
stdin instead of argv, and by bypassing the npm `.cmd` shim with the JavaScript
entrypoint through `process.execPath`.

Nekode relevance:

- Medium to high. Nekode has a Gemini runtime contract and currently maps the
  run prompt through `gemini -p <prompt>`, which can hit Windows command-line
  length limits for long launch prompts.
- This should be handled in the runtime adapter contract, not in ad hoc daemon
  launch code.

Proposed development task:

- Add a Gemini Windows launch contract that sends long prompts via stdin.
- Resolve the Gemini CLI JavaScript entrypoint when running on Windows, so the
  daemon can avoid the npm `.cmd` shim.
- Preserve current non-Windows behavior unless tests show the stdin contract is
  safe to use everywhere.

Acceptance:

- Unit tests cover Gemini prompt injection for Windows long prompts without
  placing the full prompt in argv.
- Command summaries continue to redact prompt/stdin content.
- A Windows smoke run verifies that long wake prompts no longer fail with the
  command-line length limit.

Estimated effort: 1 focused runtime-adapter slice plus Windows smoke. It can be
implemented independently from server API changes.

### 5. Plain-Text Attachment Inline Preview

Slock daemon 0.46.1 previews `.txt` / `text/plain` attachments inline.

Nekode relevance:

- Low to medium. Nekode already supports attachments and Web previews, but small
  logs and notes benefit from direct inline reading.
- This is independent from daemon protocol work and can be a small Web/server UX
  follow-up.

Proposed development task:

- Add inline preview for small `text/plain` attachments, with byte and line
  limits.
- Preserve download behavior for large text files or unknown encodings.
- Reuse existing attachment search and saved-message attachment flows.

Acceptance:

- `.txt` and `text/plain` attachments show a bounded inline preview in message
  cards.
- Large files are truncated or download-only with a clear label.
- Browser smoke covers upload, preview, search, and saved-message discovery.

Estimated effort: 1 small Web/server UX slice if existing attachment download
APIs can serve the preview text with size limits.

## Priority

| Priority | Work | Estimated effort | Reason |
| --- | --- | --- | --- |
| P0/P1 | Reply-target hints | Small | Directly reduces wrong-channel/thread replies and agent confusion. |
| P0/P1 | Membership system events | Medium | Makes channel access changes observable without agent self-reporting. |
| P0/P1 | Workspace/activity visibility scoping | Medium to large | Privacy boundary; should be server-enforced. |
| P1 | Gemini Windows stdin launch | Small plus Windows smoke | Runtime reliability for Windows Gemini agents and long prompts. |
| P2 | Plain-text attachment preview | Small | Useful UX polish; does not affect daemon correctness. |

## Suggested Task Split

| Task | Scope | Dependencies |
| --- | --- | --- |
| Daemon reply-target hint | Add computed reply target to launch prompts and delivery instructions. | Current launch prompt snapshot contract |
| Membership system events | Emit and render system events for channel membership changes. | Channel membership APIs |
| Workspace/activity visibility | Enforce shared-channel visibility for workspace, run, and activity APIs. | Channel membership model |
| Gemini Windows launch | Move Gemini Windows long prompt path to stdin and bypass npm `.cmd` shim. | Runtime adapter tests |
| Text attachment preview | Bounded inline preview for text/plain attachments in Web. | Existing attachment APIs |

## Non-Goals

- Do not change the task #212/#213/#214 IM enhancement order.
- Do not treat Slock daemon 0.46.1 behavior as automatically implemented in
  Nekode until tests and code land.
- Do not make Web-only filtering the source of truth for workspace/activity
  visibility.
- Do not remove existing target/thread fields when adding reply-target hints.
