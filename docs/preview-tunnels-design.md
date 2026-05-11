# Preview Tunnels design — stub

Status: **stub** · Will expand into full proposal after B (memory system)
lands and is signed off by codex.

## Goal

Expose a web service running on a computer via a temporary HTTPS URL,
routed through the nekode server over the existing daemon reverse connect-rpc
stream. Agents use it to share "look what I built" with reviewers
without manual port forwarding or third-party tunnels (ngrok, frp).

## Locked call boundaries

These are the invariants any implementation must honour. Everything
else (data model, RPC shape, UI layout) is open until the full doc
lands.

1. **Agent requests require human approval.** An agent calling
   `nekode-preview-open --port N` creates a tunnel record in state
   `pending_approval`. The URL exists but returns `403 pending approval`
   on request until a human with the appropriate role approves it.
   Approval surfaces as:
   - an item in the channel admin's approval inbox (web)
   - a message posted by the agent into the channel (so reviewers see
     what's waiting)
   - optionally a Telegram / IM notification through the existing IM
     binding if the human is configured that way.
2. **Human-created tunnels are active on creation.** A channel admin
   creating a tunnel from the web UI or composer command skips the
   approval state; their role is the authorisation.
3. **Computer owner can always revoke.** Regardless of who opened the
   tunnel, the computer owner and any workspace admin can close it
   immediately. Agents cannot close human-created tunnels.
4. **TTL-bounded.** Every tunnel has a max lifetime (default 2h, max
   24h). TTL extension requires the same role as initial creation:
   agents request, humans approve.
5. **Server-authoritative state.** The daemon holds no persistent
   tunnel records. On restart it queries the server for its active
   tunnels and re-establishes the reverse stream.

## What goes in the full proposal (later)

- Proto contract for tunnel registry + reverse proxy bidi stream
- HTTP + WebSocket forwarding semantics
- Rate limiting + body size caps + CSP injection
- ACL: public / require-login / per-channel-member
- Audit log schema and retention
- Web UI mockup for: approval inbox, tunnel list on ComputerDetail,
  composer slash command, agent's request flow
- Migration of the preview tool into the runtime adapter so every
  runtime (Claude / Codex / OpenCode) picks it up uniformly

Revisit this doc once B is done.
