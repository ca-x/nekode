# Stella-Style Plugin Architecture Plan

Status: next milestone planning
Related tasks: task #217
Reference baseline: CherryHQ/stella at `120eced`

This plan captures how Stella's plugin architecture should influence Nekode
after the current release. It is not part of the active task #199 release gate
and should not block task #216 verification.

## Stella Reference

Stella's useful pattern is a built-in plugin host, not unrestricted dynamic
third-party code loading.

Important mechanics:

- a plugin implements `Register(host)` and is added to a process catalog;
- the main binary blank-imports plugin packages so `init()` registration runs;
- the host exposes a flat registration surface for metadata, admin/config,
  providers, channels, runtimes, tools, hooks, memory, prompts, session env, and
  bundled skills;
- runtime work receives capability-specific contexts and plugin-scoped platform
  services rather than raw global access;
- managed channel registration composes metadata, config schema, redaction,
  status, channel spec, and runtime spec into one provider-owned unit;
- runtime host applies server/config desired state to plugin runtime instances;
- manifest plugins can later contribute binaries, prompts, session env, and
  OAuth metadata.

The most important lesson is ownership. Each provider or runtime owns its
schema, validation, redaction, runtime builder, status, and capability metadata
in one place.

## Nekode Current Gap

Nekode currently has extensible identifiers, but not plugin ownership.

Agent runtime state:

- `internal/runtimeadapter` owns runtime catalog entries, local probes,
  contracts, default templates, wrap command builders, and smoke checks;
- `cmd/nekode-daemon` builds local inventory by calling
  `runtimeadapter.ComputerInventory`;
- server-side agent creation parses runtime adapter JSON and calls
  `runtimeadapter.BuildWrapCommand`.

IM state:

- provider code has moved toward `internal/imchannels/<provider>`;
- provider schema and registry remain centralized in `internal/imadapter`;
- interaction capability planning is a follow-up from task #212 and task #213.

Protocol state:

- runtime kinds and profile adapter config are open strings/JSON, so the proto
  can carry plugin-owned data without a closed enum migration;
- there is no server-dispatched daemon probe protocol yet.

## Target Architecture

Use a server-authoritative plugin model:

1. Server owns plugin catalog, metadata, schemas, capability facts, redaction,
   config validation, and desired state.
2. Web consumes server-published schemas and capabilities; it must not become a
   second source of plugin capability truth.
3. Daemon executes only local-machine work that the server cannot do: binary
   discovery, version checks, environment presence checks, workspace probes, and
   runtime process launches.
4. Daemon reports structured probe and runtime results back to the server.
5. Provider and runtime packages own their own contract and status shape.

Keep the first version in-process and built-in. Third-party manifest or binary
plugins can come later, after the safety model and upgrade policy are explicit.

## Plugin Surfaces

### Agent Runtime Plugin

An agent runtime plugin should own:

- plugin id, for example `runtime/codex` or `runtime/claude`;
- display metadata and availability notes;
- option schema, default model, and sensitive fields;
- prompt/system-message/session/model/reasoning mapping;
- direct-run support flag and unsupported reason;
- wrap command builder;
- output parser and failure classifier;
- probe specs for binary discovery and health checks;
- capability facts such as file write, session resume, structured output, and
  lifecycle support.

### IM Channel Plugin

An IM channel plugin should own:

- plugin id, for example `channel/telegram` or `channel/weixin`;
- endpoint config schema, validation, and redaction;
- binding methods, including endpoint-mode-aware QR binding where applicable;
- callback, polling, or WebSocket runtime boundary;
- outbound send renderer and delivery status mapping;
- live-smoke metadata and release-gate status;
- interaction capabilities from the task #212 schema.

### Probe Plugin

Probe behavior should be structured data, not arbitrary shell:

```json
{
  "probeId": "runtime/claude/version",
  "commandCandidates": ["claude"],
  "args": ["--version"],
  "envNames": ["ANTHROPIC_API_KEY"],
  "cwdPolicy": "none",
  "timeoutMs": 3000,
  "maxStdoutBytes": 4096,
  "maxStderrBytes": 4096,
  "redactEnvValues": true,
  "classifiers": [
    { "type": "exit_code", "ok": [0] },
    { "type": "regex", "stream": "stdout", "pattern": "claude" }
  ]
}
```

Daemon policy must allow or reject each probe based on this structured shape.
Do not let the server send arbitrary command lines, shell fragments, or dynamic
scripts as a probe.

## Server And Daemon Boundary

Server responsibilities:

- plugin catalog and desired state;
- schemas, capability projection, redaction, and validation;
- probe plan construction;
- availability computation from probe results;
- agent profile creation and runtime contract selection;
- IM callback ingress and channel configuration APIs.

Daemon responsibilities:

- execute approved probe specs with timeouts and output limits;
- report paths, versions, health categories, and redacted diagnostics;
- run approved agent command plans;
- supervise runtime process lifecycle and report run status;
- keep local facts local unless explicitly requested by a redacted probe.

The daemon must not:

- decide product capability truth independently from server schemas;
- execute arbitrary server shell;
- leak environment variable values through probe output;
- own public IM webhook ingress unless a later edge/tunnel design explicitly
  adds that role.

## Migration Phases

### Phase 0: ADR

Create an ADR that freezes the server/daemon/Web plugin boundary, the built-in
first policy, and the no-arbitrary-shell probe rule.

### Phase 1: Registry Facade

Introduce a server-side plugin registry facade that wraps current
`runtimeadapter` and `imadapter` data without changing behavior. This phase
should make current centralized code look like built-in plugins while keeping
existing JSON/proto compatibility.

### Phase 2: Structured Probe Protocol

Add a daemon probe request/result protocol. The server sends `ProbeSpec` records
for enabled or known runtime plugins; the daemon executes approved probes and
returns structured `ProbeResult` records. The server computes runtime
availability from those results.

### Phase 3: Runtime Plugin Split

Move Codex, Claude, OpenCode, Gemini, Kimi, and custom runtime contract logic
behind runtime plugin registrations. Existing behavior must be locked by tests
before moving each runtime.

### Phase 4: IM Channel Plugin Split

Move provider schemas, send/callback/binding/runtime status, and live-smoke
metadata into IM channel plugin registrations. Align interaction capabilities
with the task #212 schema.

### Phase 5: Manifest/Binary Plugins

Evaluate a Stella-like manifest plugin layer for optional tools, prompts,
session env, OAuth, and binary installation. Treat third-party plugins as a
separate security and upgrade policy problem.

## Proposed Next Tasks

| Task | Scope | Dependency |
| --- | --- | --- |
| P1/P0-design: Stella-style plugin ADR | Server/daemon/Web boundary, built-in first, probe safety policy. | Current release complete |
| P1: Plugin registry facade | Wrap runtime and IM registries as built-in plugin records without behavior change. | ADR |
| P1: Daemon structured probe protocol | Add ProbeSpec/ProbeResult and daemon execution policy. | ADR |
| P1: Runtime plugins for current agent runtimes | Move codex/claude/opencode/gemini/kimi/custom contract logic behind plugin registration. | Registry facade and probe protocol |
| P1: IM channel plugin migration | Move provider schema/runtime/binding/status metadata into channel plugins. | Registry facade and task #212 |
| P2: Manifest plugin install policy | Optional binary/prompt/session-env plugins with explicit trust policy. | Built-in plugin model proven |

## Acceptance Criteria

The plugin milestone is successful when:

- current runtime and IM behavior remains unchanged under the plugin facade;
- Web reads capability/config schema from server plugin projections;
- daemon runtime availability can be produced from structured probe results;
- runtime contracts are testable per plugin;
- no code path allows arbitrary server-provided shell execution on daemon hosts;
- docs explain how to add a new runtime or IM channel without editing a central
  switch table.

## Non-Goals

- Do not block the current release on plugin migration.
- Do not add untrusted third-party plugin execution in the first milestone.
- Do not replace existing daemon run lifecycle, leases, or task feedback
  semantics.
- Do not move public IM webhook ingress into the daemon unless a later edge
  connectivity design explicitly requires it.
