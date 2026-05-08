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
- Container and binary build: `Dockerfile`, `docker-compose.yml`, and
  `build.sh`
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

## Build and Package

Build the Web console and a release-style Go binary:

```bash
./build.sh
./dist/nekode version
```

Useful build variables:

```bash
VERSION=v0.1.0 COMMIT="$(git rev-parse --short HEAD)" ./build.sh
GOOS=linux GOARCH=arm64 ./build.sh dist/nekode-linux-arm64
```

Build and run the local container image:

```bash
docker build \
  --build-arg VERSION=local \
  --build-arg COMMIT="$(git rev-parse --short HEAD)" \
  --build-arg BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -t nekode:local .

docker compose up --build
```

The Docker image builds the Vite Web console and stores the static output at
`/app/web/dist` inside the image. The current `nekode` server process exposes
the API on port `18790`; production deployments that need the console from the
same origin should serve `/app/web/dist` through a reverse proxy or a static
file layer until backend static serving is added.

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
