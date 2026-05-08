# Nekode Bootstrap API

Status: task #94 backend Phase 2 plus task #98 daemon/bridge implementation

Persistence uses Ent ORM. Configure the database with:

| Variable | Default | Purpose |
| --- | --- | --- |
| `NEKODE_ADDR` | `:18790` | HTTP listen address |
| `NEKODE_GRPC_ADDR` | `127.0.0.1:18789` | local daemon gRPC listen address |
| `NEKODE_DAEMON_TRANSPORT` | `grpc` | server/daemon transport lane; only `grpc` is implemented, QUIC/WebTransport is reserved |
| `NEKODE_BASE_URL` | `http://localhost:18790` | Public server URL |
| `NEKODE_DATA_DIR` | `$HOME/.nekode` | Persistent data directory |
| `NEKODE_DB_TYPE` | `sqlite` | `sqlite`, `postgres`, or `mysql` |
| `NEKODE_DB_DSN` | `$NEKODE_DATA_DIR/nekode.db` | Database DSN |
| `NEKODE_DB_PATH` | empty | Legacy sqlite path alias when `NEKODE_DB_DSN` is unset |
| `NEKODE_CACHE_DRIVER` | `badger` | Cache provider: `badger`, `redis`, or `none` |
| `NEKODE_CACHE_DIR` | `$NEKODE_DATA_DIR/cache` | Badger cache directory |
| `NEKODE_CACHE_TTL` | `5m` | Default projection/read-through TTL |
| `NEKODE_CACHE_KEY_VERSION` | `v1` | Cache key namespace version |
| `NEKODE_CACHE_REDIS_ADDR` | empty | Redis address when `NEKODE_CACHE_DRIVER=redis` |
| `NEKODE_CACHE_REDIS_USERNAME` | empty | Optional Redis username |
| `NEKODE_CACHE_REDIS_PASSWORD` | empty | Optional Redis password |
| `NEKODE_CACHE_REDIS_DB` | `0` | Redis DB number |

SQLite uses the same pure-Go `github.com/lib-x/entsqlite` driver pattern as
Nekobot.

All mutating collaboration endpoints use bearer authentication after the first
admin user is created.

## Persistence and Cache Boundary

The database remains the authority for collaboration facts. The implementation
persists:

- `collaboration_events`: append-only event log keyed by stable `server_id` and
  monotonic `sequence`, with indexes for target and aggregate replay;
- `idempotency_records`: durable deduplication for daemon RPC retries, keyed by
  scope, method, actor, and idempotency key;
- task `version` and `claim_lease_id`: CAS-based exclusive task claims;
- the first-admin bootstrap guard: a singleton idempotency row created in the
  same transaction as the first admin user.

Cache providers are optional performance infrastructure. They only store
rebuildable projection/read-through data and must not become the authority for
idempotency, leases, event sequence, task claim CAS, sessions, tokens, secrets,
or config. Cache keys must include `server_id`, `protocolVersion`, and
`NEKODE_CACHE_KEY_VERSION` so cursor/projection state is invalidated across
server or protocol boundaries. Event-driven invalidation should use the durable
event log sequence as its primary signal.

Realtime events expose this boundary directly in the daemon protocol:

- `EventCursor.server_id` carries the server identity that issued a cursor;
- `EventOperation` gives clients a stable invalidation verb such as created,
  updated, appended, state changed, heartbeat, or invalidated;
- `EventScope` gives realtime/projection consumers a first-class scope such as
  workspace, target, task, run, agent, computer, user, endpoint, or daemon.

Frontend clients should use these as cache routing hints. The event payload and
durable sequence remain authoritative.

The default cache provider is embedded Badger (`github.com/dgraph-io/badger/v4`)
under `$NEKODE_DATA_DIR/cache`. Setting `NEKODE_CACHE_DRIVER=redis` switches to
Redis and requires `NEKODE_CACHE_REDIS_ADDR`; Redis connection failure aborts
startup instead of silently weakening runtime behavior. Set
`NEKODE_CACHE_DRIVER=none` to disable the cache provider.

## Authentication

### `POST /api/auth/bootstrap`

Creates the first admin user. This endpoint only works while the user table is
empty.

Request:

```json
{
  "username": "admin",
  "password": "secret123",
  "displayName": "Admin"
}
```

Response `201`:

```json
{
  "token": "session-token",
  "expiresUnix": 1790000000,
  "user": {
    "id": "usr_...",
    "username": "admin",
    "displayName": "Admin",
    "role": "admin"
  }
}
```

### `POST /api/auth/login`

Request:

```json
{
  "username": "admin",
  "password": "secret123"
}
```

Response `200`: same shape as bootstrap.

### `POST /api/auth/logout`

Requires `Authorization: Bearer <token>`. Deletes the current session.

### `GET /api/auth/me`

Requires bearer auth. Returns the current user.

## Interaction Endpoints

Interaction endpoints are the transport-neutral extension point for Web, CLI,
API, webhook, MCP, IM, mobile, IDE, and custom clients.

### `GET /api/interaction-endpoints`

Query:

- `limit`: optional, defaults to `100`.

Response:

```json
{
  "items": [
    {
      "id": "iep_...",
      "kind": "web",
      "provider": "browser",
      "displayName": "Web Console",
      "targetPrefix": "#",
      "inboundEnabled": true,
      "outboundEnabled": true,
      "authMode": "cookie",
      "configJson": "{}"
    }
  ]
}
```

### `POST /api/interaction-endpoints`

Request:

```json
{
  "kind": "web",
  "provider": "browser",
  "displayName": "Web Console",
  "targetPrefix": "#",
  "inboundEnabled": true,
  "outboundEnabled": true,
  "authMode": "cookie",
  "configJson": "{}"
}
```

## Messages

### `GET /api/messages?target=%23general`

Query:

- `target`: required target such as `#general`, `#general:thread`, or `dm:user`.
- `limit`: optional, defaults to `50`.

### `POST /api/messages`

Request:

```json
{
  "target": "#general",
  "content": "hello",
  "role": "user",
  "sourceEndpointId": "iep_...",
  "externalMessageId": "optional-upstream-id",
  "metadataJson": "{}",
  "requestId": "optional-idempotency-key"
}
```

## Tasks

Task states are `todo`, `in_progress`, `blocked`, `in_review`, `done`, and
`canceled`. The API accepts `cancelled` as a compatibility alias and stores it
as canonical `canceled`.

### `GET /api/tasks`

Query:

- `state`: optional.
- `target`: optional.
- `limit`: optional, defaults to `100`.

### `POST /api/tasks`

Request:

```json
{
  "summary": "wire backend",
  "target": "#general",
  "state": "todo",
  "assigneeId": "usr_..."
}
```

### `PATCH /api/tasks/{id}`

Request:

```json
{
  "summary": "updated summary",
  "state": "in_progress",
  "assigneeId": "usr_..."
}
```

## Daemon Bridge

The daemon control plane starts with the HTTP server. By default HTTP listens on
`:18790`, while gRPC listens on `127.0.0.1:18789` because the current gRPC
surface is intended for trusted local daemon processes. Browser clients should
use the authenticated HTTP bridge and event stream instead of connecting to gRPC
directly.

The server/daemon transport is intentionally a replaceable lane. The MVP
supports `NEKODE_DAEMON_TRANSPORT=grpc` over HTTP/2; future transports such as
QUIC/WebTransport should reuse the same daemon RPC semantics, durable event
envelope, cursor, acknowledgement, idempotency, and lease model rather than
forking protocol behavior.

The stable server identity is persisted in `$NEKODE_DATA_DIR/server_id`. Cursor
consumers should treat `serverId` plus `protocolVersion` as the validity boundary
for replay state.

### `GET /api/daemon/info`

Requires bearer auth. Returns the daemon bridge identity and protocol range.

Response:

```json
{
  "serverId": "srv_...",
  "serverName": "Nekode",
  "protocolVersion": 1,
  "minProtocolVersion": 1,
  "maxProtocolVersion": 1,
  "grpcAddr": "127.0.0.1:18789",
  "daemonTransport": "grpc",
  "cacheDriver": "badger"
}
```

### `GET /api/daemon/agent-statuses`

Requires bearer auth.

Query:

- `agentId`: optional exact agent filter.
- `target`: optional channel/thread/DM target filter.
- `limit`: optional, defaults to `100`.

Response:

```json
{
  "items": [
    {
      "agent_id": "agent-1",
      "computer_id": "computer-1",
      "presence": 2,
      "activity_state": 3,
      "health": 1,
      "updated_time_unix": 1790000000
    }
  ],
  "nextCursor": {
    "cursor": "#general:1",
    "sequence": 1,
    "protocol_version": 1
  }
}
```

Enum fields use protobuf JSON numbers in this first bridge surface. Frontend
clients should keep the translation isolated in the API client layer.

### `GET /api/daemon/activity`

Requires bearer auth.

Query:

- `target`: optional channel/thread/DM target.
- `agentId`: optional agent filter.
- `limit`: optional, defaults to `100`.

Response shape: `{ "items": [ActivityRecord], "nextCursor": EventCursor }`.

### `GET /api/daemon/runs`

Requires bearer auth.

Query:

- `target`: optional channel/thread/DM target.
- `taskId`: optional task filter.
- `agentId`: optional agent filter.
- `limit`: optional, defaults to `100`.

Response shape: `{ "items": [Run], "nextCursor": EventCursor }`.

### `GET /api/daemon/events`

Requires bearer auth. Replays durable collaboration events after a cursor.

Query:

- `target`: optional target filter.
- `aggregateId`: optional aggregate filter.
- `sequence`: optional numeric resume sequence.
- `scope` filters are represented in the gRPC proto as `EventScope`; the HTTP
  bridge currently exposes `target`/`aggregateId` as the minimum stable subset.
- `limit`: optional, defaults to `100`.

Response shape: `{ "items": [CollaborationEvent], "nextCursor": EventCursor }`.

### `GET /api/server-events`

Requires bearer auth. Because browser `EventSource` cannot set custom headers,
this endpoint also accepts `access_token=<token>` or `token=<token>` query
parameters. Prefer bearer headers for non-browser clients.

Query:

- `cursor`: optional opaque cursor such as `#general:42`.
- `target`: optional target filter.
- `aggregateId`: optional aggregate filter.
- `sequence`: optional numeric resume sequence when `cursor` is empty.
- `limit`: optional replay batch size, defaults to `100`.

The stream uses Server-Sent Events:

```text
id: cev_...
event: message
data: {"event_id":"cev_...","kind":2,"sequence":42}
```

Idle streams emit `event: ping` roughly every 15 seconds.

Resume is explicit: the server uses the request `cursor` or numeric `sequence`
to choose the next durable event. It does not currently translate the browser
`Last-Event-ID` header back to a sequence, so browser clients should persist the
latest `data.sequence` value and reconnect with `sequence=<last-sequence>` or a
cursor containing that sequence. `target` and `aggregateId` are request filters;
they are not changed as the stream advances through events.

Each SSE event includes `kind`, `operation`, `scope`, `workspace_id` when known,
`sequence`, `aggregate_id`, and `protocol_version`. Use `operation` + `scope` to
invalidate TanStack Query-style server-state caches; do not mirror authoritative
server state into UI-only stores.

## Daemon gRPC Minimum Surface

The gRPC service is `nekode.daemon.v1.DaemonControlService` in
`proto/nekode/daemon/v1/service.proto`. The current implementation covers the
minimum daemon/bridge loop:

- computer registration, heartbeat, inventory sync, start permit acquire/release
- server info, channel and interaction endpoint listing
- message send/read
- task create/get/update/list/board/claim
- agent status update/list/profile projection
- server event and activity stream subscribe/ack
- activity log/list and event replay
- run status, run lease renewal, run step append, run fetch/list/get

Other protocol RPCs remain in the generated interface and will return the gRPC
unimplemented status until the corresponding storage/runtime slice lands.

Example `grpcurl` call:

```bash
grpcurl -plaintext \
  -import-path proto \
  -proto nekode/daemon/v1/service.proto \
  -d '{}' \
  127.0.0.1:18789 \
  nekode.daemon.v1.DaemonControlService/GetServerInfo
```
