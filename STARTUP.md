# Nekode Console Startup Guide

This branch adds a Vite + React + TypeScript console under `web/`. The app talks to the Nekode HTTP bridge through the typed client in `web/src/api.ts` and treats SSE as a cursor/invalidation signal; displayed facts still come from HTTP DTO fetch/refetch.

## Requirements

- Node.js 18+ with npm
- Nekode HTTP bridge available to the browser
- A login token from the backend auth flow

The local development default assumes the frontend dev server proxies `/api` to `http://127.0.0.1:18790`.

## Install

```bash
cd web
npm install
```

## Development

```bash
cd web
npm run dev -- --port 18791
```

Open `<http://127.0.0.1:18791/>`.

If the backend is not on `127.0.0.1:18790`, update `web/vite.config.ts` before running the dev server or serve the built files from the same origin as the backend.

## Verification

```bash
cd web
npm run typecheck
npm run build
git diff --check
```

## Production Build

```bash
cd web
npm run build
```

Vite writes static output to `web/dist/`. The generated `dist/` includes the favicon and web manifest files from `web/public/`.

## Implemented Surfaces

- Auth bootstrap and API base client.
- Typed DTO mapping in `web/src/api.ts` for backend snake_case/camelCase and proto numeric enum compatibility.
- Six-state board using canonical states: `todo`, `in_progress`, `blocked`, `in_review`, `done`, `canceled`.
- Task detail inspector with state update entry, assignee, claim lease, version, target, and timestamps.
- Durable activity/event stream view using `/api/daemon/events`.
- SSE subscription via `/api/daemon/events` with `access_token` query and `data.sequence` resume.
- Cursor invalidation when `serverId` or `protocolVersion` changes.
- Nekode logo, favicon, Apple touch icon, and web manifest assets.

## Realtime Boundary

Current Web transport is SSE only. The stable event combinations used for invalidation are:

- `message/appended/target`
- `activity/created/target`
- `task/created/task`
- `task/state_changed/task`
- `task/updated/task`
- `task/claimed/task`

Do not drive task/message facts directly from SSE payloads. Use the event as a refetch signal and render the server DTO result.

Future WebSocket support should be added behind the same API client boundary only if the browser UI needs bidirectional realtime behavior. QUIC/WebTransport is reserved for a future server-daemon transport lane, not the Web MVP path.

## File Map

```text
web/
├── index.html
├── public/                 # favicon and web manifest assets
├── src/
│   ├── App.tsx             # main UI shell, board, activity, inspector
│   ├── api.ts              # HTTP/SSE client and DTO normalization
│   ├── types.ts            # frontend DTO types
│   ├── styles.css          # app styles
│   ├── main.tsx
│   └── assets-brand.png
├── package.json
├── tsconfig.json
└── vite.config.ts
```

## Known Gaps

- Live browser session against a deployed backend was not exercised from this host.
- Drag/drop is intentionally not implemented; state changes use explicit API calls and refetch.
- No WebSocket transport is implemented yet.
