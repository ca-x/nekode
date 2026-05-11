# Reference Architecture Analysis

**Date**: 2026-05-07  
**Projects Analyzed**: open-agent-room, zano  
**Purpose**: Inform nekode architecture decisions

---

## 1. open-agent-room Architecture

### 1.1 Key Components

**Backend** (Go):
- Single binary server with embedded frontend assets
- WebSocket daemon bridge at `/daemon`
- JSON envelope protocol for messages, tasks, presence, memory
- Server-Sent Events (SSE) for browser updates
- Local daemon that spawns agent runtimes (Codex CLI, Claude Code, demo)

**Frontend** (Web):
- Real-time channel chat for humans and agents
- Agent roster with status and capabilities
- Task board with customizable lanes
- Skill Center for reusable skills
- Agent creation flow with runtime/model/prompt selection

**Protocol**:
- JSON envelope for messages, task assignment, presence, memory, replies
- Per-agent skill attachment injected into runner context
- Deterministic demo fallback for machines without local agent CLI

### 1.2 Strengths

- Simple, single-binary deployment
- Clear separation between server and daemon
- Skill Center for reusable agent capabilities
- Per-agent runtime selection
- Local-first with embedded assets

### 1.3 Lessons for Nekode

- Keep deployment simple (single binary or docker-compose)
- Separate concerns: server (HTTP/WebSocket) vs. daemon (agent lifecycle)
- Use JSON envelopes for protocol extensibility
- Support multiple runtime types (CLI, Claude Code, demo)
- Skill/capability management is important for agent UX

---

## 2. Zano Architecture

### 2.1 Key Components

**Frontend** (Next.js + Supabase):
- Channels, DMs, threads, tasks
- Agent management and creation
- Real-time subscriptions via Supabase

**Bridge** (Node.js CLI):
- Runs locally on user's machine
- Subscribes to channels via Supabase
- Spawns Claude Code subprocess for each agent
- Pipes messages in/out via `zano` CLI

**Agents** (Claude Code processes):
- Long-running processes with workspace directory
- Persistent `MEMORY.md` and `notes/` directory
- Communicate exclusively through `zano` CLI
- Access to local machine (files, tools, network)

**Database** (Supabase):
- PostgreSQL with RLS policies
- Real-time subscriptions
- Auth and user management

### 2.2 Strengths

- Fully self-hostable (Supabase + Next.js + Bridge)
- Clear separation: Web (UI) → Bridge (local) → Agents (Claude Code)
- Persistent agent memory and workspace
- Monorepo structure (pnpm + Turborepo)
- CLI-first agent communication
- RLS policies for security

### 2.3 Lessons for Nekode

- Consider Supabase-like architecture for self-hosting (DB + Auth + Realtime)
- Bridge pattern for local agent execution is clean
- CLI-first communication is powerful for agents
- Persistent workspace and memory are essential
- Monorepo structure works well for multi-package projects
- RLS policies provide fine-grained access control

---

## 3. Nekode Architecture Synthesis

### 3.1 Recommended Approach

**Combine strengths from both**:

1. **Backend** (Go, like open-agent-room):
   - Single binary or docker-compose deployment
   - HTTP + WebSocket for Web UI
   - connect-rpc for daemon/server RPC
   - JSON envelope protocol for extensibility

2. **Database** (SQLite for MVP, Supabase-like for scale):
   - Self-hosted friendly
   - Clear schema for channels, messages, tasks, agents
   - RLS-like policies for access control

3. **Bridge Pattern** (from Zano):
   - Local bridge for agent execution
   - Spawns Claude Code or other runtimes
   - Pipes messages through unified CLI

4. **Agent Communication** (CLI-first):
   - `nekode` CLI for agents to send messages, claim tasks, etc.
   - Persistent workspace and memory
   - Structured JSON for protocol

### 3.2 Multi-Channel Architecture

**Interaction Endpoints** (from Task #91 plan):
- `web`: Browser console (HTTP + WebSocket)
- `cli`: Command-line interface (connect-rpc or stdio)
- `api`: REST API (HTTP)
- `webhook`: Webhook delivery (HTTP POST)
- `mcp`: Model Context Protocol
- `bridge`: Local agent bridge (WebSocket)
- `im`: WeChat, Slack, Telegram, etc. (future)
- `mobile`: Mobile app (future)
- `ide`: IDE plugin (future)

**Unified Endpoint Pattern**:
```
POST /api/channels/{id}/messages
  - From: Web, CLI, API, Webhook, MCP, Bridge, IM, etc.
  - Message routed to channel regardless of source
  - Metadata includes source endpoint for audit/routing
```

### 3.3 Implementation Phases

**Phase 1-3** (Task #91 - Backend Bootstrap):
- Go backend with HTTP + WebSocket
- Proto definitions for daemon/server protocol
- SQLite schema (users, channels, messages, tasks, agents)
- Docker/Compose deployment
- Health check and version endpoints

**Phase 4-5** (Task #91 - Core Services):
- Auth service (JWT, API keys)
- Channel/message/task services
- Multi-channel routing logic
- WebSocket real-time delivery

**Phase 6-7** (Task #92 - Frontend):
- React/Vite frontend
- Design system and components
- Real-time message delivery
- Task board and agent management

**Phase 8+** (Task #93 - Deployment & Future):
- Deployment guide and operations
- Bridge for local agent execution
- CLI for agents
- Webhook and MCP support

---

## 4. Key Decisions for Nekode

| Decision | Rationale |
|----------|-----------|
| Go backend | Matches nekobot, single binary deployment, good performance |
| SQLite MVP | Self-hosted friendly, no external dependencies, easy to backup |
| Multi-channel from day 1 | Extensibility for CLI, API, Webhook, MCP, IM, etc. |
| Bridge pattern for agents | Clean separation, local execution, persistent workspace |
| CLI-first agent communication | Powerful, scriptable, works with existing tools |
| JSON envelope protocol | Extensible, human-readable, easy to debug |
| Monorepo structure | Shared types, coordinated releases, clear boundaries |

---

## 5. Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| Complexity of multi-channel | Start with Web + CLI, add others incrementally |
| Agent lifecycle management | Use bridge pattern, clear heartbeat/lease mechanism |
| Real-time scalability | Use WebSocket pooling, message queuing for scale |
| Database schema changes | Use migrations, test on sample data |
| Security of agent execution | Sandbox agents, limit file access, audit logging |

---

## 6. Next Steps

1. **Task #91**: Implement backend bootstrap with multi-channel foundation
2. **Task #92**: Build frontend console with real-time features
3. **Task #93**: Document deployment and operations
4. **Future**: Add bridge for local agent execution, CLI for agents, webhook/MCP support

---

**Analysis Complete**: 2026-05-07 21:30  
**Status**: Ready for implementation
