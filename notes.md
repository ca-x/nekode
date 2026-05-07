# Notes: Nekode Bootstrap

## Sources

### Nekobot reusable protocol work
- Source repo: `/home/czyt/code/go/nekobot`
- Proto reference: `proto/nekobot/daemon/v1/daemon.proto`
- Design reference: `docs/superpowers/specs/2026-05-07-slock-runtime-integration.md`
- Key points:
  - Runtime names must stay string-based to support Codex, Claude, OpenCode, and future adapters.
  - Server owns collaboration state, permissions, event replay, idempotency, tasks, reminders, DMs, and agent profiles.
  - Daemon owns local runtime discovery, process supervision, token injection, start queue, and diagnostics.
  - Memory is a curated agent recovery index plus notes, not a dump of session history.

### Current Nekode repository
- Path: `/home/czyt/code/go/nekode`
- Remote: `git@github.com:ca-x/nekode.git`
- Initial content before bootstrap: `.gitignore`, `LICENSE`
- Current bootstrap adds backend, proto, docs, Docker files, and tests.

### Reference: open-agent-room
- Local clone: `/home/czyt/code/go/references/open-agent-room`
- Stack: single Go binary with embedded frontend assets, local daemon bridge over WebSocket, SSE browser updates.
- Useful ideas:
  - humans and agents share channel history and task assignment semantics;
  - daemon protocol has explicit envelope/event types;
  - local daemon can run Codex, Claude, or deterministic demo runtime;
  - demo fallback is useful for development without real runtime credentials.

### Reference: Zano
- Local clone: `/home/czyt/code/go/references/zano`
- Stack: Next.js web app, Supabase DB/Auth/Realtime, local Node bridge, CLI for agents.
- Useful ideas:
  - bridge process is the local-machine boundary that spawns agents;
  - agents use CLI commands for message/task operations;
  - workspace memory is per-agent `MEMORY.md` plus notes;
  - channels, DMs, threads, and task board are first-class product objects.

## Synthesized Findings

### First backend boundary
- Start with health/version/protocol metadata endpoints so the frontend can connect early.
- Keep database and runtime supervisor implementation out of the first bootstrap commit.
- Generate Go protobuf stubs now so later daemon/server work can compile against the contract.
- Keep interaction endpoints transport-neutral so Web, CLI, API, webhook, MCP, IM, mobile, and IDE clients can all reuse the same message/task/DM core.

### Work split
- task #91: architecture, repo bootstrap, protocol files, backend skeleton, verification.
- task #92: frontend console UX and implementation. @小螃蟹 designs interaction details, @小吱吱 implements.
- task #93: product scope, hosted deployment plan, acceptance docs.

### Reference Architecture Analysis (2026-05-07)

**Projects Analyzed**:
- `open-agent-room` (Go backend, single binary, skill center, local daemon)
- `zano` (Next.js + Supabase frontend, Node bridge, Claude Code agents, CLI-first)

**Key Insights**:
1. **Deployment**: Single binary (open-agent-room) or docker-compose (nekode) is simpler than multi-service.
2. **Bridge Pattern**: Zano's bridge for local agent execution is clean; consider for future agent support.
3. **CLI-First**: Agents communicate via CLI (zano CLI in Zano, nekode CLI in nekode) for scriptability.
4. **Persistent Memory**: Each agent has workspace + MEMORY.md + notes/ for long-term learning.
5. **Multi-Channel**: Both projects support multiple interaction modes; nekode should design for extensibility from day 1.
6. **Protocol**: JSON envelopes (open-agent-room) or structured types (zano) work well for extensibility.

**Nekode Synthesis**:
- Go backend (HTTP + WebSocket for Web, gRPC for CLI, JSON envelope for extensibility)
- SQLite for MVP (self-hosted friendly, no external deps)
- Multi-channel architecture: Web, CLI, API, Webhook, MCP, Bridge, IM (future)
- Unified endpoint/target routing for all channel kinds
- Bridge pattern for local agent execution (future phase)
- CLI for agents to send messages, claim tasks, manage memory

**Recommended Phases**:
1. Task #91: Backend bootstrap (HTTP, WebSocket, proto, SQLite schema, Docker)
2. Task #92: Frontend console (React, real-time, task board, agent management)
3. Task #93: Deployment guide, operations, acceptance testing
4. Future: Bridge for local agents, CLI, webhook/MCP support, IM integrations
