# Nekode

Nekode is a self-hosted, Slock-style collaboration server and daemon runtime.
It is a new implementation that reuses the daemon protocol work captured from
Slock behavior, while keeping the codebase independent from Nekobot.

## Current Bootstrap

- Go backend entrypoint: `cmd/nekode`
- HTTP health endpoint: `GET /health`
- Protocol metadata endpoint: `GET /api/protocol`
- Reusable daemon IDL: `proto/nekode/daemon/v1/daemon.proto`
- Implementation design: `docs/slock-style-daemon-runtime.md`
- Logo asset: `assets/brand.png`
- Container bootstrap: `Dockerfile` and `docker-compose.yml`

## Run Locally

```bash
go run ./cmd/nekode serve --addr :18790
curl http://localhost:18790/health
```

Environment variables:

| Name | Default | Purpose |
| --- | --- | --- |
| `NEKODE_ADDR` | `:18790` | HTTP listen address |
| `NEKODE_BASE_URL` | `http://localhost:18790` | Public server URL |
| `NEKODE_DATA_DIR` | `$HOME/.nekode` | Persistent data directory |

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
