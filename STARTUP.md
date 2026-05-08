# Nekode Console - Startup & Environment Guide

## Overview

Nekode Console is a React + TypeScript web application for managing daemon-based agent collaboration. It provides real-time task boards, messaging, and agent lifecycle control through a modern web UI.

## Prerequisites

- Node.js 18+ (LTS recommended)
- npm 9+
- Backend daemon running on `localhost:50051` (gRPC)
- HTTP bridge running on `localhost:8080` (for API/SSE)

## Development Setup

### 1. Install Dependencies

```bash
cd web
npm install
```

### 2. Environment Configuration

Create `.env.local` in the `web/` directory:

```env
# Backend API endpoint (HTTP bridge)
VITE_API_BASE_URL=http://localhost:8080

# Daemon gRPC endpoint (for direct connections if needed)
VITE_DAEMON_HOST=localhost
VITE_DAEMON_PORT=50051

# Optional: Enable debug logging
VITE_DEBUG=false
```

### 3. Start Development Server

```bash
npm run dev
```

The console will be available at `http://localhost:5173` (Vite default).

## Build for Production

```bash
npm run build
```

Output is in `dist/` directory. Serve with any static HTTP server:

```bash
npx serve dist
```

## Architecture

### Frontend Stack

- **React 18** - UI framework
- **TypeScript** - Type safety
- **TanStack Query (React Query)** - Server state management
- **Zustand** - Client-only UI state
- **Vite** - Build tool and dev server

### API Integration

The frontend communicates with the backend through:

1. **HTTP REST API** (`/api/daemon/*`)
   - Task CRUD operations
   - Message sending
   - Agent control
   - Attachment upload/download

2. **Server-Sent Events (SSE)** (`/api/daemon/events`)
   - Real-time task updates
   - Message delivery
   - Agent status changes
   - Cursor-based resume for reconnection

### State Management

- **Server State (TanStack Query)**
  - Tasks, messages, agents, channels
  - Automatic cache invalidation on mutations
  - Cursor-based pagination for large datasets

- **Client State (Zustand)**
  - UI visibility (panels, modals)
  - Temporary form inputs
  - Scroll positions
  - Theme preferences

## Key Features

### Task Board

- 6-state lifecycle: `todo` → `in_progress` → `blocked` → `in_review` → `done` → `canceled`
- Drag-and-drop task movement (future)
- Real-time updates via SSE
- Task detail inspector with full history

### Messaging

- Channel-based conversations
- Thread support for focused discussions
- Message search and filtering
- Attachment support (images, files)

### Agent Control

- Start/stop/restart agent runtimes
- Direct messaging to agents
- Agent status monitoring
- Capability and permission display

### Real-Time Updates

- SSE connection with automatic reconnection
- Cursor-based event replay for missed updates
- Optimistic UI updates with server reconciliation
- Event operation hints for cache invalidation

## API Endpoints

### Daemon Info

```
GET /api/daemon/info
```

Returns server metadata, protocol version, and server ID.

### Events Stream

```
GET /api/daemon/events?access_token=<token>&cursor=<cursor>
```

Server-Sent Events stream for real-time updates. Supports cursor-based resume.

### Task Operations

```
POST /api/daemon/tasks
GET /api/daemon/tasks/:id
PATCH /api/daemon/tasks/:id
GET /api/daemon/tasks/board
POST /api/daemon/tasks/:id/claim
```

### Message Operations

```
POST /api/daemon/messages
GET /api/daemon/messages
GET /api/daemon/channels
```

### Agent Operations

```
POST /api/daemon/agents/:id/control
GET /api/daemon/agents/:id/status
POST /api/daemon/agents/:id/message
```

## Troubleshooting

### Connection Issues

**Problem**: "Failed to connect to daemon"

**Solution**:
1. Verify backend is running: `curl http://localhost:8080/api/daemon/info`
2. Check VITE_API_BASE_URL in `.env.local`
3. Ensure CORS is enabled on backend

### SSE Reconnection

**Problem**: "SSE connection dropped, retrying..."

**Solution**:
1. Check browser console for error details
2. Verify server is still running
3. Check network tab for 5xx errors
4. Cursor should auto-resume from last sequence

### Build Errors

**Problem**: TypeScript compilation errors

**Solution**:
```bash
npm run type-check
npm run lint
```

### Performance Issues

**Problem**: Slow task board rendering

**Solution**:
1. Check browser DevTools Performance tab
2. Verify TanStack Query cache is working (Network tab)
3. Reduce number of visible tasks with filters
4. Check for memory leaks in DevTools

## Development Workflow

### Type Safety

```bash
npm run type-check    # Check TypeScript
npm run lint          # Run ESLint
npm run format        # Format code
```

### Testing

```bash
npm run test          # Run tests (when available)
npm run test:watch    # Watch mode
```

### Code Organization

```
web/src/
├── App.tsx           # Main component
├── main.tsx          # Entry point
├── api.ts            # API client and DTO mapping
├── types.ts          # TypeScript type definitions
├── styles.css        # Global styles
└── assets-brand.png  # Brand assets
```

## Protocol Extensibility

The frontend is designed to support multiple transport protocols:

- **Current**: SSE (Server-Sent Events) for real-time updates
- **Future**: WebSocket for bidirectional communication
- **Future**: QUIC for daemon-to-daemon communication

The transport boundary is abstracted in `api.ts`, allowing protocol changes without affecting UI components.

## Deployment

### Static Hosting

1. Build: `npm run build`
2. Upload `dist/` to CDN or static host
3. Configure backend API endpoint via environment variables or runtime config

### Docker

```dockerfile
FROM node:18-alpine AS builder
WORKDIR /app
COPY web/ .
RUN npm install && npm run build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/nginx.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
```

### Environment Variables

Set at build time or runtime:

- `VITE_API_BASE_URL` - Backend API endpoint
- `VITE_DAEMON_HOST` - Daemon hostname (for future direct connections)
- `VITE_DAEMON_PORT` - Daemon port (for future direct connections)
- `VITE_DEBUG` - Enable debug logging

## Support & Documentation

- **Frontend Style Guide**: See `FRONTEND_STYLE.md` for UI/UX conventions
- **API Documentation**: Backend proto files in `proto/nekode/daemon/v1/`
- **Architecture**: See `docs/` for design decisions and architecture notes
