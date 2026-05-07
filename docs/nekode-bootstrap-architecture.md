# Nekode Bootstrap Architecture

## Purpose

Nekode is a new self-hosted Slock-style collaboration system. It should be
hostable by the user, independent from older application code, and compatible
with the reusable daemon/server behavior captured in the protocol document.

## Initial Repository Shape

```text
cmd/nekode/                 CLI entrypoint
internal/config/            environment and flag-backed configuration
internal/server/            HTTP server and bootstrap API endpoints
internal/version/           build metadata
proto/nekode/daemon/v1/     daemon/server protobuf contract
gen/go/nekode/daemon/v1/    generated Go bindings
docs/                       implementation design and architecture notes
assets/brand.png            logo asset for later UI work
```

## Bootstrap API

The first server exposes only the endpoints needed to verify deployment and
show the protocol boundary:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health` | process health and version |
| `GET` | `/api/version` | build metadata |
| `GET` | `/api/protocol` | daemon protocol doc and proto location |

## Interaction Endpoint Model

Web is not the only future client. Nekode reserves protocol space for
interaction endpoints so later Web UI, CLI, public API, webhook, MCP, IM,
mobile, and IDE integrations all write through the same collaboration model.

The first protobuf contract includes:

- `InteractionEndpoint` for transport/provider/auth/capability metadata;
- `ListInteractionEndpoints` for daemon and client discovery;
- `source_endpoint_id`, external message id, and metadata JSON on messages;
- endpoint references on channel records.

Core server code should treat endpoints as ingress/egress adapters. They
authenticate the sender and annotate messages, but do not own separate task, DM,
or channel semantics.

## Protocol Boundary

The protobuf file at `proto/nekode/daemon/v1/daemon.proto` is the authoritative
IDL for the daemon/server control plane. Nekode uses project-local package names
but preserves the reusable field numbers and RPC semantics from the Slock-style
protocol work.

The behavioral contract lives in `docs/slock-style-daemon-runtime.md`. Later
implementation should follow that document for:

- computer registration and heartbeat;
- runtime discovery and `agent:start` processing;
- channel, thread, task, DM, reminder, attachment, and event replay semantics;
- agent profile, status, direct message, and lifecycle control;
- memory boundaries and startup diagnostics.

## Near-Term Implementation Order

1. Define persistent storage tables for server-owned objects.
2. Implement auth/session/membership primitives.
3. Implement interaction endpoint registration and permission checks.
4. Implement channel, thread, DM, message, and task board APIs.
5. Implement daemon registration, heartbeat, and event replay.
6. Implement runtime start queue, token file injection, and status reporting.
7. Wire the frontend console against stable REST/gRPC gateway endpoints.

## Verification Baseline

Bootstrap commits should pass:

```bash
buf lint
buf generate
go test ./...
go build ./...
git diff --check
```
