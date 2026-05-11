# Nekode

Nekode is a self-hosted, Slock-style collaboration server and daemon runtime.
It is a new implementation that reuses the daemon protocol work captured from
Slock behavior, while keeping the codebase independent from Nekobot.

## Current Bootstrap

- Go backend entrypoint: `cmd/nekode`
- Go daemon entrypoint: `cmd/nekode-daemon`
- HTTP health endpoint: `GET /health`
- Protocol metadata endpoint: `GET /api/protocol`
- Phase 2 API docs: `docs/api.md`
- Reusable daemon IDL: `proto/nekode/daemon/v1/*.proto`
- Protocol capability review: `docs/protocol-capability-review.md`
- Implementation design: `docs/slock-style-daemon-runtime.md`
- IM channel integration and deployment notes:
  `docs/im-channel-integration.md`
- Real IM provider runtime plan:
  `docs/im-real-provider-runtime-plan.md`
- Next milestone IM capability plan:
  `docs/im-capability-next-milestone-plan.md`
- Next milestone Stella-style plugin architecture plan:
  `docs/stella-plugin-architecture-plan.md`
- Slock daemon 0.46.1 parity plan:
  `docs/slock-daemon-0.46.1-parity-plan.md`
- Web console assets: `web/src/assets-brand.png` and `web/public/*`
- Container and binary build: `Dockerfile`, `docker-compose.yml`, and
  `build.sh`
- Local references for future design comparison:
  `/home/czyt/code/go/references/open-agent-room` and
  `/home/czyt/code/go/references/zano`

## IM Channel Reference

Nekode's IM endpoint and channel-adapter work references
[CherryHQ/stella](https://github.com/CherryHQ/stella), an MIT-licensed
self-hosted assistant with Telegram, QQ, Feishu, WeChat, and Terminal channel
runtimes.
The intended reuse is Stella's proven channel shape: platform-specific adapters
normalize inbound events, a shared coordinator handles identity/routing/session
behavior, and platform renderers handle streaming/output details. Nekode keeps
the implementation on its existing primitives: `InteractionEndpoint`,
`storage.Message`, `SourceEndpointID`, `ExternalMessageID`, message attachment
IDs, metadata JSON, and the existing attachment/message/notification storage
paths. Platform adapters and channel-add/binding UI for Telegram, QQ, Feishu,
WeChat, and Terminal may reuse Stella's channel configuration, validation,
onboarding, and runtime structure where compatible, with source attribution
retained in documentation.

The integration model is endpoint-centric rather than a separate IM chat
system:

1. Each IM channel instance is an `InteractionEndpoint`, for example
   `kind=im` with `provider=feishu`, `weixin`, `telegram`, or `qq`.
2. Platform adapters verify inbound events, deduplicate by platform message id,
   normalize sender/conversation/content into Nekode's inbound IM DTO, and use
   the existing attachment path for media.
3. Message coordination maps endpoint identity and external conversations onto
   existing Nekode targets, threads, sessions, users, agents, and commands. IM
   adapters must not call daemon runtimes directly.
4. Inbound messages persist as existing `storage.Message` records with
   `SourceEndpointID`, `ExternalMessageID`, attachment IDs, and metadata JSON so
   Web, daemon, tasks, runs, search, saved messages, and activity all see the
   same collaboration facts.
5. Outbound delivery renders existing Nekode messages, activities, and
   notifications back to bound IM endpoints through delivery records and retry
   state; platform adapters only translate the delivery into provider API calls.
6. Group behavior starts conservative (`mention` by default, with `always` and
   `disabled` available) and can later add per-group agent, system prompt, and
   tool policy overrides.

Current status: the repository has provider schemas, normalizers, outbound
delivery lifecycle, frame/render helpers, UI configuration, mock fixtures, and
thin provider runtimes for Terminal, Telegram, Feishu, QQ, Weixin, and
ServerChan. These runtimes are locally verified with mocked provider APIs or
fake SDK boundaries, but external providers are still not production-connected
until operator-owned credentials and public callback or polling environments
pass live smoke. See `docs/im-real-provider-runtime-plan.md` for the task #205
live-smoke matrix and `Not-tested` provider caveats.

Next milestone IM work is tracked separately in
`docs/im-capability-next-milestone-plan.md`. That plan covers provider
interaction capabilities, weak-channel fallback policy, and Telegram rich
interactions. It does not change the current release gate.

The medium-term plugin architecture direction is tracked in
`docs/stella-plugin-architecture-plan.md`. That plan evaluates Stella-style
built-in plugin registration for agent runtimes, IM channels, and structured
server-dispatched daemon probes. It is a post-release architecture migration
plan, not current release scope.

Slock daemon 0.46.1 parity follow-up is tracked in
`docs/slock-daemon-0.46.1-parity-plan.md`. That plan evaluates reply-target
hints, membership system events, workspace/activity visibility scoping, Gemini
Windows stdin launch, and text/plain attachment previews for Nekode.

## Run Locally

```bash
go run ./cmd/nekode serve --addr :18790
curl http://localhost:18790/health
```

Run the minimal daemon client against a local server:

```bash
go run ./cmd/nekode-daemon run --config ~/.nekode/daemon.json --once
```

Daemon enrollment is token based. Use the authenticated HTTP API to create an
enrollment; the server returns a generated install token and a short-lived
one-line install command. The copied URL contains only a one-time install code,
not the daemon token. When the script endpoint is fetched, the server rotates
the daemon token, writes only its hash, and embeds the new token in the script
body. The daemon sends that token as a connect-rpc bearer header. The token is
not a manually configured global server secret.

The daemon config is generated by the install/enrollment flow and contains the
server-issued install token. For local smoke tests, `--token` or
`NEKODE_DAEMON_TOKEN` can override the config value.

The daemon onboarding flow is:

1. Create a Computer enrollment in Web; the server generates a device token and
   install command.
2. Run the install command on the target machine. Web shows platform-specific
   entries. Linux/macOS use a Teleport-style one-liner such as
   `sudo bash -c "$(curl -fsSL <server>/api/daemon/enrollments/<id>/install.sh?code=<code>)"`.
   Windows uses `install.ps1`. The script body contains the device token,
   server addresses, computer identity, and GitHub Releases daemon download
   source; the copied command URL does not expose the daemon token. Script
   responses are `no-store`, and install codes are one-time/short-lived.
3. `nekode-daemon` starts,
   reads the generated config, sends the token as a connect-rpc bearer header, and
   registers/heartbeats automatically.
4. Web polls the enrollment/Computer state until the daemon connects, then
   allows the operator to continue.

Agent creation is separate from daemon installation. Web should use the
connected Computer inventory to choose a runtime/capability and create one or
more agent instances; a runtime kind is not unique and may back multiple agents.

Daemon RPC runs over connect-rpc on the main HTTP listener. Long-lived streams
such as preview tunnels require end-to-end HTTP/2 or h2c; do not place the
daemon path behind a proxy that downgrades requests to HTTP/1.1.

Release builds publish `nekode_${version}_${os}_${arch}` and
`nekode-daemon_${version}_${os}_${arch}` artifacts for Linux, macOS, and
Windows, plus a multi-arch server Docker image at `czyt/nekode:<version>` and
`ghcr.io/ca-x/nekode:<version>` for Linux amd64/arm64. The default Docker image
runs only the server; daemon installation should download the platform-specific
daemon artifact from GitHub Releases instead of pulling it from the server
runtime image. Private deployments can add a download-source override without
changing the server image.

Daemon management scripts are exposed without enrollment tokens for already
installed machines:

- `GET /api/daemon/scripts/upgrade.sh`
- `GET /api/daemon/scripts/reinstall.sh`
- `GET /api/daemon/scripts/uninstall.sh`
- `GET /api/daemon/scripts/upgrade.ps1`
- `GET /api/daemon/scripts/reinstall.ps1`
- `GET /api/daemon/scripts/uninstall.ps1`

Upgrade and reinstall scripts keep the existing daemon config, download the
selected release artifact, verify `SHA256SUMS.txt`, replace the binary, and
restart the service. Uninstall removes the service and binary; set
`NEKODE_PURGE_CONFIG=1` to also remove local daemon config. The scripts follow
the installer's Teleport-style shape: root/admin assertion, clear logging,
platform and architecture detection, checksum verification, service manager
integration, and explicit cleanup behavior.

Environment variables:

| Name | Default | Purpose |
| --- | --- | --- |
| `NEKODE_ADDR` | `:18790` | HTTP listen address |
| `NEKODE_DAEMON_RPC_URL` | empty | public connect-rpc URL for daemon installers; defaults to `NEKODE_BASE_URL` |
| `NEKODE_DAEMON_CONFIG` | `$HOME/.nekode/daemon.json` | daemon install config path |
| `NEKODE_DAEMON_TOKEN` | empty | local daemon token override for smoke tests |
| `NEKODE_BASE_URL` | `http://localhost:18790` | Public server URL |
| `NEKODE_WEB_DIST_DIR` | empty | Optional external Web console dist directory; the binary serves embedded assets by default |
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
./dist/nekode-daemon version
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

The Docker image and `build.sh` embed the Vite Web console into the `nekode`
binary. The server exposes the Web console and API from the same port
(`18790`). Set `NEKODE_WEB_DIST_DIR` or `--web-dist-dir` only when you want to
override the embedded assets with an external static directory during
development.

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
