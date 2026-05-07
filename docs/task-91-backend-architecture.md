# Task #91: Backend Architecture And Repo Bootstrap

## Goal

Bootstrap Nekode as an independent, self-hosted Slock-style implementation with
a reusable daemon protocol, minimal Go backend, container baseline, and clear
handoff points for frontend and product/deployment work.

## Scope

This task owns:

- repository bootstrap under `/home/czyt/code/go/nekode`;
- Go module and backend entrypoint;
- health, version, and protocol metadata endpoints;
- protobuf import/generation for daemon/server collaboration semantics;
- explicit protocol extension point for non-Web interaction channels;
- Docker and Compose baseline;
- plan-with-files working artifacts;
- bootstrap verification.

This task does not own the full frontend console or final hosted deployment
guide. Those stay in task #92 and task #93.

## Architecture Decisions

| Area | Decision | Reason |
| --- | --- | --- |
| Module | `github.com/ca-x/nekode` | Matches the GitHub repository import path. |
| Backend | Go standard library HTTP for bootstrap | Keeps the first slice small and avoids locking into a framework too early. |
| Protocol package | `nekode.daemon.v1` | Reuses Slock-style field/RPC semantics while removing old application naming. |
| Proto generation | `buf` + Go/gRPC stubs | Keeps the protocol first-class and testable. |
| Data store | SQLite planned, not implemented in bootstrap | Self-hosted friendly; schema should be added when domain services land. |
| Branding | `assets/brand.png` copied from the Nekobot logo asset | Matches the requested visual lineage for later UI work. |

## Interaction Endpoint Extensibility

Nekode must not assume that users only talk to agents through the Web UI. The
protocol reserves `InteractionEndpoint` as the abstraction for every ingress or
egress surface:

- `web`: browser console with HTTP/WebSocket;
- `cli`: local or remote command-line client;
- `api`: REST/gRPC clients;
- `webhook`: third-party automation callbacks;
- `mcp`: model/tool clients;
- `im`: WeChat, Slack, Telegram, DingTalk, Lark, and similar transports;
- `mobile` and `ide`: future first-party clients;
- `custom`: private integrations.

Messages carry source endpoint metadata, external message id, and opaque
metadata JSON. Core task, DM, channel, thread, and agent direct-message logic
should remain transport-neutral.

## Bootstrap API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health` | Process health and version. |
| `GET` | `/api/version` | Build metadata. |
| `GET` | `/api/protocol` | Protocol document and proto path. |

## Reference Inputs

The following repositories were cloned locally for architecture comparison:

| Project | Local path | What to study |
| --- | --- | --- |
| open-agent-room | `/home/czyt/code/go/references/open-agent-room` | Go single-binary shape, daemon WebSocket bridge, SSE updates, demo runtime fallback. |
| Zano | `/home/czyt/code/go/references/zano` | Web/bridge/CLI split, channels/DMs/tasks product model, per-agent workspace memory. |

Use these as design references, not source-code templates.

## Follow-Up Backend Phases

1. Add persistent storage and migrations for users, memberships, endpoints,
   channels, DMs, messages, tasks, reminders, attachments, events, computers,
   runtimes, and agents.
2. Add auth and permission middleware with join-to-write enforcement.
3. Implement endpoint registration, bearer/webhook/MCP token validation, and
   endpoint-scoped audit metadata.
4. Implement channel/thread/DM/message/task APIs.
5. Implement daemon registration, heartbeat, event replay, and leases.
6. Implement runtime discovery, start queue, token file injection, and agent
   status reporting.
7. Add WebSocket/SSE and REST/gRPC gateway surfaces for the frontend and CLI.

## Verification

Bootstrap gate:

```bash
buf lint
buf generate
go test ./...
go build ./...
git diff --check
```

## Acceptance Criteria

- The repository builds from a clean clone.
- The generated Go protobuf files compile.
- `/health`, `/api/version`, and `/api/protocol` are covered by tests.
- The protocol exposes non-Web interaction endpoint extension points.
- Docker and Compose provide a clear first deployment path.
- task #92 and task #93 can proceed without guessing the protocol location or
  initial API shape.
