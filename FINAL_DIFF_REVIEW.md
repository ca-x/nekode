# Nekode Console Final Diff Review

## Summary

Branch `task92-frontend-console` adds the Nekode Web console under `web/`. It is a new Vite + React + TypeScript frontend and does not modify backend implementation files.

Latest pushed commit: `e9110d2`.

## Changed Files

- `.gitignore`: ignores `node_modules/` and `web/dist/`.
- `web/package.json`, `web/package-lock.json`: frontend package and locked dependencies.
- `web/tsconfig*.json`, `web/vite.config.ts`: TypeScript and Vite configuration.
- `web/index.html`: app entry plus favicon / Apple touch icon / web manifest links.
- `web/public/*`: favicon, PNG icon sizes, Apple touch icon, and `site.webmanifest`.
- `web/src/main.tsx`: React entry point.
- `web/src/App.tsx`: console UI, navigation, board, activity stream, daemon overview, task inspector.
- `web/src/api.ts`: HTTP client, DTO normalization, EventSource/SSE subscription boundary.
- `web/src/types.ts`: normalized frontend DTO types.
- `web/src/styles.css`: responsive console styling.
- `web/src/assets-brand.png`: supplied Nekode logo.
- `web/src/vite-env.d.ts`: Vite env declarations.

## Implemented Features

- Real HTTP API client for auth, messages, tasks, task board, interaction endpoints, protocol info, daemon info, and daemon events.
- API-layer normalization for proto numeric enum values, snake_case/camelCase fields, and `cancelled` input alias to canonical `canceled`.
- SSE subscription with `access_token` query, explicit `cursor` / `sequence` resume, and transport-neutral event DTOs.
- Cursor state persisted locally and cleared when `serverId` or `protocolVersion` changes.
- Six-state task board in fixed order: `todo -> in_progress -> blocked -> in_review -> done -> canceled`.
- Task inspector with status change API entry, ID, target, assignee, claim lease, version, and timestamps.
- Activity/Event Stream view using `/api/daemon/events`.
- Invalidation rules narrowed to the stable task #107 event producer combinations.
- Supplied logo plus favicon / PWA icon assets.

## Important Boundaries

- Server DTO/refetch remains authoritative. SSE is only a cursor/invalidation signal.
- Components consume normalized camelCase frontend DTOs and canonical task states.
- Proto enum number handling and snake_case compatibility stay in `web/src/api.ts`.
- Web MVP uses SSE. WebSocket can be added later behind the API client boundary if bidirectional realtime is needed.
- QUIC/WebTransport is not in the Web path; it belongs to a future server-daemon transport lane.

## Verification

Run from `/tmp/nekode-task92/web`:

```bash
npm run typecheck
npm run build
git diff --check
```

All three passed before commit and push.

Build output included:

- `dist/index.html`
- `dist/assets/assets-brand-sudAxwjW.png`
- `dist/assets/index-BNh2SAPe.css`
- `dist/assets/index-DmYRMfTO.js`
- copied public favicon / manifest files at the dist root

## Not Tested

- Live deployed backend session from a browser.
- Real auth token and SSE resume against a production server.

## Merge Notes

- The branch is pushed to `origin/task92-frontend-console`.
- There are no backend migrations or protocol changes in this frontend commit.
- If these docs are committed separately, keep them aligned with the actual dependency set. The current code does not use TanStack Query, Zustand, ESLint, or a Dockerfile.
