# Notes: Nekode Bootstrap

## Sources

### Nekobot reusable protocol work
- Source repo: `/home/czyt/code/go/nekobot`
- Proto reference: `proto/nekobot/daemon/v1/daemon.proto`
- Design reference: `docs/superpowers/specs/2026-05-07-slock-runtime-integration.md`
- Key points:
  - Runtime names must stay string-based to support Codex, Claude, OpenCode, and future adapters.
  - Server owns collaboration state, permissions, event replay, idempotency, tasks, reminders, DMs, and agent profiles.
  - Daemon owns local runtime discovery, process supervision, token injection, start queue, and diagnostics.
  - Memory is a curated agent recovery index plus notes, not a dump of session history.

### Current Nekode repository
- Path: `/home/czyt/code/go/nekode`
- Remote: `git@github.com:ca-x/nekode.git`
- Initial content before bootstrap: `.gitignore`, `LICENSE`
- Current bootstrap adds backend, proto, docs, Docker files, and tests.

## Synthesized Findings

### First backend boundary
- Start with health/version/protocol metadata endpoints so the frontend can connect early.
- Keep database and runtime supervisor implementation out of the first bootstrap commit.
- Generate Go protobuf stubs now so later daemon/server work can compile against the contract.

### Work split
- task #91: architecture, repo bootstrap, protocol files, backend skeleton, verification.
- task #92: frontend console UX and implementation. @小螃蟹 designs interaction details, @小吱吱 implements.
- task #93: product scope, hosted deployment plan, acceptance docs.
