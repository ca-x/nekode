# Nekode Bootstrap API

Status: task #94 backend Phase 2 plus task #98 daemon/bridge implementation,
with task #115 first-admin setup support.

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
| `NEKODE_BOOTSTRAP_ADMIN_USERNAME` | empty | Optional first-admin username for unattended bootstrap |
| `NEKODE_BOOTSTRAP_ADMIN_PASSWORD` | empty | Optional first-admin password for unattended bootstrap |
| `NEKODE_BOOTSTRAP_ADMIN_NAME` | empty | Optional first-admin display name |
| `NEKODE_BOOTSTRAP_DISABLE_WEB` | `false` | Disable browser setup while still allowing env bootstrap |

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

### `GET /api/auth/setup-status`

Returns the first-run setup state. This endpoint is intentionally readable
before login so the Web console can choose between login, setup, or the
operator-only disabled setup message.

`GET /api/auth/init-status` is kept as a compatibility alias.

Response `200`:

```json
{
  "initialized": false,
  "webSetupEnabled": true,
  "bootstrapMethods": ["env", "web"],
  "serverId": "srv_...",
  "dataDir": "/home/user/.nekode"
}
```

### `POST /api/auth/bootstrap`

Creates the first admin user. This endpoint only works while the user table is
empty and browser setup is enabled.

`POST /api/auth/init` is the first-run Web setup alias and returns the same
response shape.

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

Repeat bootstrap attempts return `409 {"error":"already_initialized"}`. If
`NEKODE_BOOTSTRAP_DISABLE_WEB=true` and the server is not initialized, browser
bootstrap returns `403 {"error":"web setup is disabled"}`.

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

## Notification Routes

Notification routes map Nekode targets or threads to dedicated outbound
interaction endpoints. They are preferences and routing facts only; provider
send runtimes still consume `OutboundDelivery` records.

### `GET /api/notification-routes`

Query:

- `target`: optional target filter.
- `threadId`: optional thread filter.
- `endpointId`: optional endpoint filter.
- `eventKind`: optional event kind such as `message`, `mention`, `task`,
  `reminder`, `run`, `activity`, `delivery_status`, or `all`.
- `enabled`: optional boolean.
- `limit`: optional, defaults to `100`.

### `POST /api/notification-routes`

Request:

```json
{
  "target": "#general",
  "threadId": "optional-thread-id",
  "endpointId": "iep_...",
  "eventKind": "message",
  "preference": "all",
  "enabled": true,
  "configJson": "{}"
}
```

`preference` accepts `all`, `mentions`, or `muted`.

### `GET /api/notification-routes/resolve`

Query:

- `target`: required target.
- `threadId`: optional thread.
- `eventKind`: required event kind.
- `limit`: optional, defaults to `100`.

Returns the enabled routes that should receive that event, with thread-specific
routes preferred over target defaults and duplicate endpoints collapsed.
Routes with `enabled=false` or `preference=muted` are omitted. If both a target
route and a thread route point at the same endpoint, the thread route wins.

Response:

```json
{
  "items": [
    {
      "id": "nroute_...",
      "target": "#general",
      "threadId": "optional-thread-id",
      "endpointId": "iep_...",
      "eventKind": "message",
      "preference": "all",
      "enabled": true,
      "configJson": "{}"
    }
  ]
}
```

### `PATCH /api/notification-routes/{id}`

Patchable fields are `eventKind`, `preference`, `enabled`, and `configJson`.

## Messages

### `GET /api/messages?target=%23general`

Query:

- `target`: required target such as `#general` or `dm:user`.
- `threadId`: optional parent message id. When omitted, only parent-channel messages are returned.
- `limit`: optional, defaults to `50`.

### `POST /api/messages`

Request:

```json
{
  "target": "#general",
  "threadId": "optional-parent-message-id",
  "content": "hello",
  "role": "user",
  "sourceEndpointId": "iep_...",
  "externalMessageId": "optional-upstream-id",
  "metadataJson": "{}",
  "requestId": "optional-idempotency-key"
}
```

## Inbox

Thread inbox APIs are Web/HTTP state only and do not add daemon proto fields.

### `GET /api/inbox/threads`

Query:

- `targetPrefix`: optional target prefix such as `#` or `dm:`.
- `limit`: optional, defaults to `100`.

Returns thread rows sorted by latest reply. Each row includes `target`, `threadId`, `topic`,
`firstMessage`, `latestMessage`, `messageCount`, `unreadCount`, and read-state fields.

### `POST /api/inbox/threads/{threadId}/read`

Request:

```json
{
  "target": "#general"
}
```

Marks one thread read for the signed-in user.

### `POST /api/inbox/threads/read-all`

Request:

```json
{
  "targetPrefix": "#"
}
```

Marks all currently listed inbox threads read for the signed-in user. Omit `targetPrefix`
to mark all visible thread inbox rows read.

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
  "description": "connect the HTTP bridge to the daemon event log",
  "target": "#general",
  "state": "todo",
  "assigneeId": "usr_...",
  "blockedReason": ""
}
```

### `PATCH /api/tasks/{id}`

Request:

```json
{
  "summary": "updated summary",
  "description": "daemon bridge is connected; waiting for review",
  "state": "in_progress",
  "assigneeId": "usr_...",
  "blockedReason": "waiting for credentials"
}
```

### `GET /api/tasks/{id}/comments`

Requires bearer auth. Returns task-scoped message comments where `threadId`
equals the task id.

Query:

- `limit`: optional, defaults to `100`.

Response shape: `{ "items": [Message] }`.

### `POST /api/tasks/{id}/comments`

Requires bearer auth. Creates a task-scoped human comment on the task target and
records a durable message event with the task id as aggregate id.

Request:

```json
{
  "content": "Reviewer asked for timeline evidence.",
  "requestId": "optional-idempotency-key"
}
```

### `GET /api/tasks/{id}/timeline`

Requires bearer auth. Returns durable collaboration events for the task aggregate
so the Web inspector can show task creation, updates, state changes, and comments
without treating local UI state as authoritative.

Query:

- `sequence`: optional resume sequence.
- `limit`: optional, defaults to `100`.

Response shape: `{ "items": [CollaborationEvent], "nextCursor": EventCursor }`.

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

Daemon gRPC authentication is enrollment based. The server generates a daemon
install token when a user starts adding a Computer, stores only a token hash
under `$NEKODE_DATA_DIR/daemon_enrollments`, and returns the full token once in
the install command. Daemons send the token as gRPC metadata
`authorization: Bearer <token>`. Missing, unknown, or expired tokens are rejected
with `Unauthenticated`.

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

### `POST /api/daemon/enrollments`

Requires bearer auth. Creates a pending daemon enrollment and returns a generated
install token plus a Teleport-style install command. The copied command URL
contains a short-lived install code, not the daemon token. The script endpoint
rotates the daemon token before rendering the script body; operators should not
hand-configure a global daemon token.

Request:

```json
{
  "displayName": "Office Mac mini",
  "computerId": "computer-office",
  "hostname": "office-mini",
  "expiresUnix": 1790000000
}
```

Response:

```json
{
  "id": "den_...",
  "tokenPrefix": "ndt_abc12345",
  "token": "ndt_...",
  "installCommand": "sudo bash -c \"$(curl -fsSL https://nekode.example/api/daemon/enrollments/den_.../install.sh?code=ndi_...&platform=linux)\"",
  "installScriptUrl": "https://nekode.example/api/daemon/enrollments/den_.../install.sh?code=ndi_...&platform=linux",
  "statusUrl": "/api/daemon/enrollments/den_...",
  "status": "pending",
  "computerId": "computer-office"
}
```

### `GET /api/daemon/enrollments/{id}/install.sh`

Public install endpoint guarded by the enrollment install `code` query
parameter. It returns a Linux/macOS shell installer and does not require bearer
auth because it is executed on a new machine before daemon registration.

Query:

- `code`: required short-lived one-time install code.
- `platform`: optional UI hint such as `linux` or `darwin`; the shell script
  still detects OS and architecture locally.

Security behavior:

- Response header `Cache-Control: no-store`.
- The install code can be consumed once and expires with the enrollment or
  after the short install window.
- The server rotates the daemon token when the script is rendered, stores only
  the hash, and embeds the new token in the script config body.
- The copied URL never contains the daemon token.

The generated script detects OS/architecture, downloads
`nekode-daemon_${version}_${os}_${arch}.tar.gz` from GitHub Releases or
`NEKODE_DAEMON_DOWNLOAD_BASE_URL`, verifies `SHA256SUMS.txt`, writes
`/etc/nekode/daemon.json` with mode `0600`, and installs a systemd or launchd
service when available.

### `GET /api/daemon/enrollments/{id}/install.ps1`

Public install endpoint guarded by the same one-time `code` query parameter. It
returns a Windows PowerShell installer that downloads
`nekode-daemon_${version}_windows_amd64.zip`, verifies `SHA256SUMS.txt`, writes
the daemon config under `%ProgramData%\Nekode`, and installs/starts the
`nekode-daemon` Windows Service.

### `GET /api/daemon/scripts/{action}.sh`

Public daemon management script endpoint for Linux/macOS. Supported actions:

- `upgrade`: keep the existing config, download and verify the selected daemon
  release artifact, replace the binary, and restart the service.
- `reinstall`: keep the existing config, remove and recreate the service,
  download and verify the selected daemon release artifact, and restart.
- `uninstall`: stop/disable the service and remove the daemon binary.

These scripts do not require bearer auth and do not contain enrollment install
codes or daemon tokens. Upgrade/reinstall require an existing daemon config at
`/etc/nekode/daemon.json` by default. Uninstall preserves that config unless
`NEKODE_PURGE_CONFIG=1` is set.

Script environment overrides:

- `NEKODE_DAEMON_VERSION`: release version to download.
- `NEKODE_DAEMON_DOWNLOAD_BASE_URL`: release artifact base URL.
- `NEKODE_DAEMON_BIN_PATH`: daemon binary path, default
  `/usr/local/bin/nekode-daemon`.
- `NEKODE_DAEMON_CONFIG_PATH`: config path, default
  `/etc/nekode/daemon.json`.
- `NEKODE_DAEMON_SERVICE_NAME`: systemd service name, default
  `nekode-daemon`.
- `NEKODE_PURGE_CONFIG`: set to `1` during uninstall to remove config.

### `GET /api/daemon/scripts/{action}.ps1`

Public daemon management script endpoint for Windows PowerShell. Supported
actions are the same `upgrade`, `reinstall`, and `uninstall` operations. The
script downloads `nekode-daemon_${version}_windows_amd64.zip`, verifies
`SHA256SUMS.txt`, manages the `nekode-daemon` Windows Service, and keeps the
existing `%ProgramData%\Nekode\daemon.json` config unless
`NEKODE_PURGE_CONFIG=1` is set during uninstall.

Additional Windows overrides:

- `NEKODE_DAEMON_INSTALL_DIR`: daemon binary directory, default
  `%ProgramFiles%\Nekode`.
- `NEKODE_DAEMON_CONFIG_DIR`: daemon config directory, default
  `%ProgramData%\Nekode`.

### `POST /api/daemon/enrollments/{id}/revoke`

Requires bearer auth. Revokes a pending enrollment, clears the stored daemon
token hash and install-code hash, and returns status `revoked`. After this call,
the previous install URL cannot render a script and the previous daemon token is
rejected by gRPC authentication.

### `GET /api/daemon/enrollments/{id}`

Requires bearer auth. Returns the enrollment status for Web polling. The full
token, install code, and install command are intentionally omitted from this
status response.
Successful daemon `RegisterComputer`, `HeartbeatComputer`, or inventory sync
marks the enrollment `connected` and records the reported computer/daemon
identity.

Response shape:

```json
{
  "id": "den_...",
  "tokenPrefix": "ndt_abc12345",
  "statusUrl": "/api/daemon/enrollments/den_...",
  "status": "connected",
  "computerId": "computer-office",
  "daemonId": "daemon-office",
  "hostname": "office-mini",
  "connectedUnix": 1790000020,
  "lastHeartbeatUnix": 1790000020
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
  -H "authorization: Bearer $NEKODE_DAEMON_INSTALL_TOKEN" \
  -import-path proto \
  -proto nekode/daemon/v1/service.proto \
  -d '{}' \
  127.0.0.1:18789 \
  nekode.daemon.v1.DaemonControlService/GetServerInfo
```
