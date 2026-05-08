# Nekode Product Scope, Deployment, and Acceptance

Status: task #93 implementation baseline
Baseline: `origin/main@10ecb3f`
Audience: implementers, reviewers, and deployment operators

This document defines what the current Nekode mainline can be accepted as,
what is intentionally deferred, and how to run a repeatable smoke test before
calling a deployment usable.

## Current Product Scope

Nekode is a self-hosted Slock-style collaboration server with a local daemon
control plane and an operational Web console. It is not a hosted SaaS control
plane yet.

### Implemented MVP

- First-admin bootstrap, login, logout, and current-user session APIs.
- Interaction endpoint registry for Web/API/daemon-style integrations.
- Message create/list APIs for channel, thread, and DM-style targets.
- Task create/list/update APIs with canonical states:
  `todo`, `in_progress`, `blocked`, `in_review`, `done`, `canceled`.
- Daemon gRPC minimum bridge for registration/heartbeat, message/task access,
  server event/activity streams, task board, task claim, agent status, runs,
  and activity/event replay.
- Durable collaboration event log with server identity, monotonic sequence,
  operation, and scope metadata.
- Durable idempotency records for retried daemon/server writes.
- Task claim/version state stored in the database rather than the browser.
- Pluggable cache interface with Badger default, Redis option, and `none`
  mode. Cache is projection/read-through infrastructure only.
- React/Vite Web console under `web/` with:
  - auth flow;
  - typed API/DTO mapping in `web/src/api.ts`;
  - SSE cursor/invalidation boundary;
  - six-state board;
  - task detail inspector;
  - activity/event stream;
  - daemon/server overview;
  - Nekode logo, favicon, and web manifest assets.

### Explicit Non-Goals for This Milestone

- Hosted multi-tenant SaaS operations.
- Browser WebSocket transport.
- Browser QUIC/WebTransport.
- Full daemon weak-network QUIC transport implementation.
- Rich task workspace features such as descriptions, comments, attachments,
  reactions, subscriptions, hidden columns, bulk operations, and board/list
  switching.
- Full permission UX and action-level denial presentation.
- Production observability stack, backups, and automated upgrades.

## Hosted vs. Self-Hosted Boundary

### Self-Hosted MVP

The current deliverable is self-hosted:

- one `nekode` server process;
- local or containerized persistent data directory;
- SQLite by default, with Postgres/MySQL DSN support through the storage layer;
- embedded Badger cache by default;
- browser console served as static Vite output or from a same-origin static
  host;
- local daemon gRPC listener intended for trusted daemon processes.

The operator owns TLS termination, DNS, reverse proxy, backups, and external
monitoring.

### Hosted Future

Hosted deployment needs separate product and security work:

- tenant/workspace account model;
- external auth and billing/limits;
- public daemon enrollment flow;
- managed storage, backup, and observability;
- upgrade orchestration;
- stricter per-action permission UX;
- tenant-aware operations playbooks.

Do not treat the current single-server configuration as a hosted SaaS design.

## Runtime and Configuration

### Backend

Run locally:

```bash
go run ./cmd/nekode serve --addr :18790 --grpc-addr 127.0.0.1:18789
```

Health check:

```bash
curl http://127.0.0.1:18790/health
```

Important environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `NEKODE_ADDR` | `:18790` | HTTP listen address |
| `NEKODE_GRPC_ADDR` | `127.0.0.1:18789` | local daemon gRPC listen address |
| `NEKODE_DAEMON_TRANSPORT` | `grpc` | daemon transport lane; only `grpc` is implemented |
| `NEKODE_BASE_URL` | `http://localhost:18790` | public server URL used by clients |
| `NEKODE_DATA_DIR` | `$HOME/.nekode` | persistent data directory |
| `NEKODE_DB_TYPE` | `sqlite` | `sqlite`, `postgres`, or `mysql` |
| `NEKODE_DB_DSN` | `$NEKODE_DATA_DIR/nekode.db` | database DSN |
| `NEKODE_DB_PATH` | empty | legacy SQLite path alias if DSN is unset |
| `NEKODE_CACHE_DRIVER` | `badger` | `badger`, `redis`, or `none` |
| `NEKODE_CACHE_DIR` | `$NEKODE_DATA_DIR/cache` | Badger cache directory |
| `NEKODE_CACHE_TTL` | `5m` | projection/read-through cache TTL |
| `NEKODE_CACHE_KEY_VERSION` | `v1` | cache namespace version |
| `NEKODE_CACHE_REDIS_ADDR` | empty | required when cache driver is `redis` |

### Frontend

Install and run development server:

```bash
cd web
npm install
npm run dev -- --port 18791
```

The Vite dev server proxies `/api` and `/health` to
`http://127.0.0.1:18790`.

Build static output:

```bash
cd web
npm run build
```

Output is written to `web/dist/`. The build includes public favicon and web
manifest files at the dist root.

### Container Bootstrap

The current `Dockerfile` builds the Go server. `docker-compose.yml` starts a
single server with `/data` persisted:

```bash
docker compose up --build
```

The Web console is currently a separate Vite app under `web/`. A production
container or reverse-proxy setup must either serve `web/dist/` on the same
origin as the API or configure equivalent `/api` routing.

## Authority and Cache Rules

- Database and durable event log are authoritative.
- Browser state is not authoritative for task state, assignee, claim lease,
  event sequence, idempotency, sessions, tokens, secrets, or config.
- SSE events are invalidation/cursor signals, not direct UI facts.
- Cache providers store rebuildable projections only.
- Cache keys must include server identity, protocol version, and cache key
  version for projection validity.

## Web Realtime Acceptance

The Web console should recognize these stable event combinations:

- `message/appended/target`
- `activity/created/target`
- `task/created/task`
- `task/state_changed/task`
- `task/updated/task`
- `task/claimed/task`

The UI should refetch server DTOs after these signals. `task/updated/task`
means non-state task updates; claims should be treated as `task/claimed/task`.

## Smoke Test Checklist

Use a fresh data directory to avoid confusing old state with acceptance data:

```bash
export NEKODE_DATA_DIR="$(mktemp -d)"
go run ./cmd/nekode serve --addr :18790 --grpc-addr 127.0.0.1:18789
```

Then open a second shell.
The examples below use `jq` for token and task ID extraction.

### 1. Backend Health and Protocol

```bash
curl -fsS http://127.0.0.1:18790/health
curl -fsS http://127.0.0.1:18790/api/protocol
```

Acceptance:

- health returns success;
- protocol metadata is readable without auth.

### 2. Bootstrap and Login

```bash
TOKEN="$(
  curl -fsS http://127.0.0.1:18790/api/auth/bootstrap \
    -H 'content-type: application/json' \
    -d '{"username":"admin","password":"secret123","displayName":"Admin"}' |
  jq -r .token
)"
curl -fsS http://127.0.0.1:18790/api/auth/me \
  -H "authorization: Bearer $TOKEN"
```

Acceptance:

- first bootstrap creates an admin and returns a token;
- `/api/auth/me` returns the admin user;
- a second bootstrap attempt is rejected.

### 3. Message and Task APIs

```bash
curl -fsS http://127.0.0.1:18790/api/messages \
  -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' \
  -d '{"target":"#general","content":"hello from smoke","role":"user","requestId":"smoke-message-1"}'

TASK_ID="$(
  curl -fsS http://127.0.0.1:18790/api/tasks \
    -H "authorization: Bearer $TOKEN" \
    -H 'content-type: application/json' \
    -d '{"summary":"smoke task","target":"#general","state":"todo"}' |
  jq -r .id
)"

curl -fsS "http://127.0.0.1:18790/api/tasks/$TASK_ID" \
  -X PATCH \
  -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' \
  -d '{"state":"blocked"}'

curl -fsS 'http://127.0.0.1:18790/api/tasks?target=%23general' \
  -H "authorization: Bearer $TOKEN"
```

Acceptance:

- message create succeeds;
- task create succeeds;
- task patch returns canonical `blocked`;
- list returns the task under `#general`;
- invalid states return `400`.

### 4. Daemon Info and Durable Events

```bash
curl -fsS http://127.0.0.1:18790/api/daemon/info \
  -H "authorization: Bearer $TOKEN"

curl -fsS 'http://127.0.0.1:18790/api/daemon/events?target=%23general&limit=20' \
  -H "authorization: Bearer $TOKEN"
```

Acceptance:

- daemon info includes `serverId`, protocol version, gRPC address, transport,
  and cache driver;
- event list includes message/task/activity-related durable events;
- `sequence` values are monotonic.

### 5. SSE Resume

Manual command-line smoke:

```bash
timeout 10s curl -N \
  "http://127.0.0.1:18790/api/server-events?access_token=$TOKEN&target=%23general&limit=5"
```

Acceptance:

- stream emits durable events or pings;
- event `data` includes `sequence`;
- reconnecting with `sequence=<last-sequence>` does not replay older events.

### 6. Web Console Smoke

```bash
cd web
npm install
npm run dev -- --port 18791
```

Open `http://127.0.0.1:18791/`.

Acceptance:

- bootstrap/login works;
- Overview shows daemon/server identity;
- Board shows six fixed states in order;
- creating or updating a task is reflected after server refetch;
- blocked and canceled are visually distinct;
- Activity view shows durable event rows;
- Task inspector shows state, target, assignee, claim lease, version, and
  timestamps;
- refreshing the page preserves valid SSE cursor state unless server identity
  or protocol version changes.

## Release Gate

Before tagging or deploying:

- `buf lint`
- `go test ./... -count=1 -timeout=180s`
- `go build ./...`
- `cd web && npm ci && npm run typecheck && npm run build`
- `git diff --check`
- smoke checklist above completed against the intended deploy target

## Remaining Backlog

These are not blockers for the current MVP baseline:

- query-cache architecture and UI-only state extraction;
- task filtering/sorting/list view/column controls/bulk operations;
- richer task detail workspace: description, comments/thread link, blocked
  reason editor, attachments, reactions, subscriptions;
- agent/runtime detail pages with run/step logs and lease/heartbeat/offline
  UX;
- action-level permission hints and settings pages;
- hosted deployment productization;
- WebSocket transport for browser bidirectional realtime;
- QUIC/WebTransport for server-daemon weak-network transport lane.
