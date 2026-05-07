# Task #91: Backend Architecture & Bootstrap

**Status**: Planning  
**Owner**: @小皮丘  
**Support**: @小螃蟹  
**Target Completion**: 2026-05-10  
**Priority**: P0 (Blocking for #92 and #93)

---

## 1. Objective

Establish the core backend architecture for nekode (self-hosted Slock.ai implementation) with:
- Complete service skeleton and project structure
- Proto definitions and RPC interfaces
- Database schema and migrations
- Docker/Compose deployment configuration
- Health check and startup verification
- Clear acceptance criteria for handoff to frontend team

---

## 2. Scope & Deliverables

### 2.1 Core Services

| Service | Purpose | Status |
|---------|---------|--------|
| **daemon** | Core Slock-style daemon (protocol, state, lifecycle) | To implement |
| **server** | Multi-channel API server (HTTP, WebSocket, gRPC, Webhook) | To implement |
| **storage** | SQLite database layer with migrations | To implement |
| **auth** | Authentication and authorization | To implement |
| **im** | IM channel/thread/message routing with multi-channel support | To implement |
| **notification** | Event notification and delivery across channels | To implement |
| **channel** | Channel kind abstraction (Web, CLI, API, Webhook, MCP, etc.) | To implement |

### 2.2 Proto Definitions

Reuse and extend from nekobot:
- `daemon.proto` - Core daemon protocol
- `server.proto` - Server API definitions
- `message.proto` - Message and thread structures
- `channel.proto` - Channel and IM routing
- `user.proto` - User and authorization
- `notification.proto` - Event and notification types

### 2.3 Database Schema

**Core Tables**:
- `users` - User accounts and profiles
- `channels` - IM channels
- `threads` - Message threads
- `messages` - Message content and metadata
- `tasks` - Task board items
- `attachments` - File attachments
- `notifications` - Notification queue
- `authorization_licenses` - License management
- `server_install_id` - Installation binding

### 2.4 API Endpoints

**Multi-Channel Architecture**:
- Web UI (HTTP + WebSocket)
- CLI Client (gRPC)
- REST API (HTTP)
- Webhook (HTTP POST)
- MCP (Model Context Protocol)
- Future channels (IDE plugins, mobile, etc.)

All channels route through unified endpoint/target system:
- Messages → `channels/{id}/messages`
- DMs → `dm/{user_id}/messages`
- Tasks → `tasks/{id}`
- Agent Direct Messages → `agent/{agent_id}/messages`

**Authentication**:
- `POST /api/auth/login` - User login
- `POST /api/auth/logout` - User logout
- `POST /api/auth/refresh` - Token refresh

**Channels**:
- `GET /api/channels` - List channels
- `POST /api/channels` - Create channel
- `GET /api/channels/{id}` - Get channel details
- `POST /api/channels/{id}/members` - Add member

**Messages** (unified endpoint for all channel kinds):
- `GET /api/channels/{id}/messages` - List messages
- `POST /api/channels/{id}/messages` - Send message (from any channel kind)
- `GET /api/threads/{id}/messages` - List thread messages
- `POST /api/threads/{id}/messages` - Reply in thread

**Direct Messages** (unified for all channel kinds):
- `GET /api/dm/{user_id}/messages` - List DM messages
- `POST /api/dm/{user_id}/messages` - Send DM

**Tasks**:
- `GET /api/tasks` - List tasks
- `POST /api/tasks` - Create task
- `PATCH /api/tasks/{id}` - Update task status

**Agent Direct Messages**:
- `GET /api/agent/{agent_id}/messages` - List agent messages
- `POST /api/agent/{agent_id}/messages` - Send to agent

**WebSocket**:
- `WS /api/ws` - Real-time message delivery (Web channel)

**gRPC** (CLI channel):
- `service NekoBot { ... }` - CLI protocol

**Webhook**:
- `POST /api/webhook/{channel_kind}` - Webhook delivery

### 2.5 Configuration

**Environment Variables**:
```
NEKODE_PORT=8080
NEKODE_BIND_HOST=0.0.0.0
NEKODE_DB_PATH=./data/nekode.db
NEKODE_LOG_LEVEL=info
NEKODE_JWT_SECRET=<generated>
NEKODE_ADMIN_TOKEN=<generated>
```

**Docker Compose**:
- nekode service (Go backend)
- SQLite volume mount
- Port mapping (8080)
- Health check endpoint

---

## 3. Implementation Plan

### Phase 1: Project Structure & Proto (2-3 hours)

**Tasks**:
- [ ] Create Go module structure (`cmd/`, `internal/`, `pkg/`)
- [ ] Set up proto build pipeline (buf.yaml, buf.gen.yaml)
- [ ] Define core proto files (daemon, server, message, channel, user)
- [ ] Generate Go code from proto
- [ ] Create proto documentation

**Acceptance**:
- Proto files compile without errors
- Generated Go code is in `gen/` directory
- All proto definitions documented

### Phase 2: Database Layer (2-3 hours)

**Tasks**:
- [ ] Design SQLite schema (users, channels, messages, threads, tasks, etc.)
- [ ] Create migration files (using golang-migrate or similar)
- [ ] Implement database initialization
- [ ] Create data access layer (DAL) interfaces
- [ ] Implement basic CRUD operations

**Acceptance**:
- Database schema documented
- Migrations run successfully
- DAL interfaces defined and tested
- Sample data can be inserted and queried

### Phase 3: Core Services (3-4 hours)

**Tasks**:
- [ ] Implement daemon service (lifecycle, state management)
- [ ] Implement server service (HTTP router, middleware)
- [ ] Implement auth service (JWT, token validation, API keys)
- [ ] Implement channel service (channel CRUD, member management)
- [ ] Implement message service (message CRUD, thread handling, multi-channel routing)
- [ ] Implement task service (task CRUD, status transitions)
- [ ] Implement channel kind abstraction (Web, CLI, API, Webhook, MCP)
- [ ] Implement message routing logic (unified endpoint for all channel kinds)

**Acceptance**:
- All services have clear interfaces
- Services can be initialized and shut down gracefully
- Multi-channel routing works correctly
- Basic unit tests for each service
- Service dependencies are injected
- Channel kind configuration is flexible

### Phase 4: API Endpoints (2-3 hours)

**Tasks**:
- [ ] Implement authentication endpoints
- [ ] Implement channel endpoints
- [ ] Implement message endpoints
- [ ] Implement task endpoints
- [ ] Add request validation and error handling
- [ ] Add request/response logging

**Acceptance**:
- All endpoints respond with correct status codes
- Request validation works
- Error responses are consistent
- API documentation (OpenAPI/Swagger) generated

### Phase 5: WebSocket & Real-time (2-3 hours)

**Tasks**:
- [ ] Implement WebSocket connection handler
- [ ] Implement message broadcast logic
- [ ] Implement connection lifecycle management
- [ ] Add reconnection handling
- [ ] Add message queue for offline delivery

**Acceptance**:
- WebSocket connections can be established
- Messages are delivered in real-time
- Disconnections are handled gracefully
- Offline messages are queued

### Phase 6: Docker & Deployment (1-2 hours)

**Tasks**:
- [ ] Create Dockerfile with multi-stage build
- [ ] Create docker-compose.yml with services
- [ ] Implement health check endpoint
- [ ] Add startup verification script
- [ ] Document deployment process

**Acceptance**:
- Docker image builds successfully
- docker-compose up starts all services
- Health check endpoint responds
- Services are accessible from host

### Phase 7: Testing & Documentation (2-3 hours)

**Tasks**:
- [ ] Write unit tests for all services
- [ ] Write integration tests for API endpoints
- [ ] Write end-to-end tests for key flows
- [ ] Create API documentation
- [ ] Create deployment guide
- [ ] Create development guide

**Acceptance**:
- Test coverage > 70%
- All tests pass
- Documentation is complete and accurate
- New developers can set up and run the project

---

## 4. Multi-Channel Architecture Design

### 4.1 Channel Kind Abstraction

**Core Concept**: All user interactions (Web, CLI, API, Webhook, MCP, etc.) are treated as "channel kinds" that route through unified endpoints.

**Channel Kind Types**:
- `web` - Web UI (HTTP + WebSocket)
- `cli` - Command-line interface (gRPC)
- `api` - REST API (HTTP)
- `webhook` - Webhook delivery (HTTP POST)
- `mcp` - Model Context Protocol
- `mobile` - Mobile app (future)
- `ide` - IDE plugin (future)

**Unified Endpoint Pattern**:
```
POST /api/channels/{channel_id}/messages
  - From: Web UI, CLI, API, Webhook, MCP
  - Message routed to channel regardless of source
  - Metadata includes channel_kind for audit/routing

POST /api/dm/{user_id}/messages
  - From: Any channel kind
  - DM delivered to user's subscribed channels

POST /api/agent/{agent_id}/messages
  - From: Any channel kind
  - Agent message routed to agent's inbox
```

### 4.2 Message Routing

**Message Flow**:
```
┌─────────────────────────────────────────────────────┐
│  Incoming Message (from any channel kind)           │
├─────────────────────────────────────────────────────┤
│                                                     │
│  1. Authenticate (JWT, API key, webhook signature) │
│  2. Validate channel_kind and permissions          │
│  3. Store message in database                      │
│  4. Route to target (channel, DM, task, agent)     │
│  5. Deliver to subscribed channels                 │
│  6. Queue for offline delivery if needed           │
│                                                     │
└─────────────────────────────────────────────────────┘
```

**Delivery Targets**:
- Channel messages → All members of channel
- DM messages → Target user's subscribed channels
- Task updates → Assigned user and watchers
- Agent messages → Agent's inbox

### 4.3 Channel Kind Configuration

**Database Schema**:
```sql
CREATE TABLE channel_kinds (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  enabled BOOLEAN DEFAULT true,
  config JSON,
  created_at TIMESTAMP,
  updated_at TIMESTAMP
);

CREATE TABLE user_channel_subscriptions (
  user_id TEXT,
  channel_kind TEXT,
  config JSON,
  created_at TIMESTAMP,
  PRIMARY KEY (user_id, channel_kind)
);
```

**Configuration Example**:
```json
{
  "channel_kinds": {
    "web": {
      "enabled": true,
      "protocol": "websocket",
      "endpoint": "/api/ws"
    },
    "cli": {
      "enabled": true,
      "protocol": "grpc",
      "endpoint": "localhost:50051"
    },
    "api": {
      "enabled": true,
      "protocol": "http",
      "endpoint": "/api"
    },
    "webhook": {
      "enabled": true,
      "protocol": "http",
      "endpoint": "/api/webhook"
    },
    "mcp": {
      "enabled": true,
      "protocol": "mcp",
      "endpoint": "/api/mcp"
    }
  }
}
```

### 4.4 Proto Extensions

**New Proto Definitions**:
```protobuf
// proto/nekode/channel/v1/channel_kind.proto
message ChannelKind {
  string id = 1;
  string name = 2;
  bool enabled = 3;
  map<string, string> config = 4;
}

// proto/nekode/message/v1/message.proto (extended)
message Message {
  string id = 1;
  string channel_id = 2;
  string user_id = 3;
  string content = 4;
  string channel_kind = 5;  // NEW: source channel kind
  map<string, string> metadata = 6;  // NEW: channel-specific metadata
  int64 created_at = 7;
  int64 updated_at = 8;
}
```

---

## 4. Technical Decisions

### 4.1 Framework & Libraries

**HTTP Server**: `github.com/gin-gonic/gin`
- Lightweight, fast, good middleware support
- Familiar to team from nekobot

**Database**: SQLite with `github.com/mattn/go-sqlite3`
- Self-contained, no external dependencies
- Suitable for self-hosted deployment
- Easy to backup and migrate

**Authentication**: JWT with `github.com/golang-jwt/jwt`
- Stateless, scalable
- Standard approach

**WebSocket**: `github.com/gorilla/websocket`
- Mature, well-tested
- Good performance

**Logging**: `go.uber.org/zap`
- Structured logging
- Consistent with nekobot

**Proto**: Protocol Buffers with `buf` CLI
- Consistent with nekobot
- Better code generation

### 4.2 Architecture Patterns

**Service Layer Pattern**:
- Each domain (auth, channel, message, task) has a service interface
- Services depend on DAL and other services
- Dependency injection via constructor

**Repository Pattern**:
- DAL interfaces for each entity
- Implementations for SQLite
- Easy to mock for testing

**Middleware Pattern**:
- Authentication middleware
- Logging middleware
- Error handling middleware
- CORS middleware

---

## 5. Acceptance Criteria

### 5.1 Code Quality

- [ ] All code follows Go conventions (gofmt, golint)
- [ ] No hardcoded secrets or credentials
- [ ] Error handling is consistent
- [ ] Logging is structured and useful
- [ ] Code is documented with comments

### 5.2 Functionality

- [ ] All proto files compile
- [ ] Database schema is created and migrations work
- [ ] All API endpoints respond correctly
- [ ] WebSocket connections work
- [ ] Authentication and authorization work
- [ ] Error responses are consistent

### 5.3 Testing

- [ ] Unit tests for all services (>70% coverage)
- [ ] Integration tests for API endpoints
- [ ] End-to-end tests for key flows
- [ ] All tests pass locally and in CI

### 5.4 Deployment

- [ ] Docker image builds successfully
- [ ] docker-compose up works
- [ ] Health check endpoint responds
- [ ] Services are accessible
- [ ] Logs are visible and useful

### 5.5 Documentation

- [ ] API documentation (OpenAPI/Swagger)
- [ ] Database schema documented
- [ ] Deployment guide written
- [ ] Development guide written
- [ ] Proto definitions documented

---

## 6. Risks & Mitigation

| Risk | Impact | Mitigation |
|------|--------|-----------|
| Proto changes break compatibility | Blocking for frontend | Version proto, use backward-compatible changes |
| Database schema changes | Data loss | Use migrations, test on sample data |
| WebSocket scalability | Performance issues | Use connection pooling, load testing |
| Authentication complexity | Security issues | Use standard JWT, security review |
| Docker build failures | Deployment blocked | Test locally first, use multi-stage builds |

---

## 7. Dependencies & Blockers

**External Dependencies**:
- Go 1.21+
- SQLite 3.x
- Docker & Docker Compose
- buf CLI for proto generation

**Internal Dependencies**:
- Proto definitions from nekobot (reuse)
- Design decisions from Task #93 (product scope)

**Blockers**:
- None identified at this stage

---

## 8. Success Metrics

- [ ] All acceptance criteria met
- [ ] Code review approved by @小螃蟹
- [ ] All tests passing
- [ ] Documentation complete
- [ ] Frontend team can start Task #92 with clear API contracts
- [ ] Deployment team can start Task #93 with working backend

---

## 9. Timeline

| Phase | Duration | Start | End |
|-------|----------|-------|-----|
| 1 | 2-3h | 2026-05-08 | 2026-05-08 |
| 2 | 2-3h | 2026-05-08 | 2026-05-08 |
| 3 | 3-4h | 2026-05-08 | 2026-05-09 |
| 4 | 2-3h | 2026-05-09 | 2026-05-09 |
| 5 | 2-3h | 2026-05-09 | 2026-05-09 |
| 6 | 1-2h | 2026-05-09 | 2026-05-10 |
| 7 | 2-3h | 2026-05-10 | 2026-05-10 |
| **Total** | **16-21h** | **2026-05-08** | **2026-05-10** |

**Target**: Complete by 2026-05-10 EOD

---

## 10. Handoff Criteria

Before handing off to Task #92 (Frontend):

- [ ] All API endpoints documented and tested
- [ ] WebSocket protocol documented
- [ ] Authentication flow documented
- [ ] Error response format documented
- [ ] Sample API requests/responses provided
- [ ] Backend running and accessible
- [ ] Frontend team can start integration

Before handing off to Task #93 (Deployment):

- [ ] Docker image builds successfully
- [ ] docker-compose.yml is complete
- [ ] Health check endpoint works
- [ ] Deployment guide is written
- [ ] Configuration documented
- [ ] Backup/restore procedures documented

---

## 11. Notes

- Reuse proto definitions from nekobot where possible
- Follow nekobot's code structure and conventions
- Use same logging and error handling patterns
- Coordinate with @小螃蟹 on API design
- Coordinate with @小吱吱 on frontend integration points
- Keep deployment simple (single docker-compose file)

---

**Created**: 2026-05-07  
**Last Updated**: 2026-05-07  
**Status**: Ready for Implementation
