# Nekode

Nekode is a self-hosted, Slock-style collaboration server and daemon runtime.
It is a new implementation that reuses the daemon protocol work captured from
Slock behavior, while keeping the codebase independent from Nekobot.

## Current Bootstrap

- Go backend entrypoint: `cmd/nekode`
- HTTP health endpoint: `GET /health`
- Protocol metadata endpoint: `GET /api/protocol`
- Phase 2 API docs: `docs/api.md`
- Reusable daemon IDL: `proto/nekode/daemon/v1/*.proto`
- Protocol capability review: `docs/protocol-capability-review.md`
- Implementation design: `docs/slock-style-daemon-runtime.md`
- Web console assets: `web/src/assets-brand.png` and `web/public/*`
- Container bootstrap: `Dockerfile` and `docker-compose.yml`
- Local references for future design comparison:
  `/home/czyt/code/go/references/open-agent-room` and
  `/home/czyt/code/go/references/zano`

## Run Locally

```bash
go run ./cmd/nekode serve --addr :18790 --grpc-addr 127.0.0.1:18789
curl http://localhost:18790/health
```

Environment variables:

| Name | Default | Purpose |
| --- | --- | --- |
| `NEKODE_ADDR` | `:18790` | HTTP listen address |
| `NEKODE_GRPC_ADDR` | `127.0.0.1:18789` | local daemon gRPC listen address |
| `NEKODE_BASE_URL` | `http://localhost:18790` | Public server URL |
| `NEKODE_DATA_DIR` | `$HOME/.nekode` | Persistent data directory |
| `NEKODE_DB_TYPE` | `sqlite` | Database type: `sqlite`, `postgres`, or `mysql` |
| `NEKODE_DB_DSN` | `$NEKODE_DATA_DIR/nekode.db` | Database DSN |
| `NEKODE_DB_PATH` | empty | Legacy SQLite path alias if `NEKODE_DB_DSN` is unset |
| `NEKODE_CACHE_DRIVER` | `badger` | Cache provider: `badger`, `redis`, or `none` |
| `NEKODE_CACHE_DIR` | `$NEKODE_DATA_DIR/cache` | Badger cache directory |
| `NEKODE_CACHE_TTL` | `5m` | Default projection/read-through cache TTL |
| `NEKODE_CACHE_REDIS_ADDR` | empty | Redis address when using the Redis cache provider |

The cache is a rebuildable projection/read-through layer. The database and
durable event log remain authoritative for idempotency, leases, event sequence,
task claims, sessions, secrets, and config.

## Protocol

The first implementation target is the daemon/server control plane described in
`docs/slock-style-daemon-runtime.md`. Generate Go bindings with:

```bash
buf generate
```

The protobuf package is `nekode.daemon.v1`. Field numbers and RPC semantics are
kept aligned with the reusable Slock-style IDL produced from the Nekobot
protocol work.

## Development Checks

```bash
buf lint
buf generate
go test ./...
go build ./...
git diff --check
```
