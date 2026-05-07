# Nekode Bootstrap API

Status: task #94 backend Phase 2

All mutating collaboration endpoints use bearer authentication after the first
admin user is created.

## Authentication

### `POST /api/auth/bootstrap`

Creates the first admin user. This endpoint only works while the user table is
empty.

Request:

```json
{
  "username": "admin",
  "password": "secret123",
  "displayName": "Admin"
}
```

Response `201`:

```json
{
  "token": "session-token",
  "expiresUnix": 1790000000,
  "user": {
    "id": "usr_...",
    "username": "admin",
    "displayName": "Admin",
    "role": "admin"
  }
}
```

### `POST /api/auth/login`

Request:

```json
{
  "username": "admin",
  "password": "secret123"
}
```

Response `200`: same shape as bootstrap.

### `POST /api/auth/logout`

Requires `Authorization: Bearer <token>`. Deletes the current session.

### `GET /api/auth/me`

Requires bearer auth. Returns the current user.

## Interaction Endpoints

Interaction endpoints are the transport-neutral extension point for Web, CLI,
API, webhook, MCP, IM, mobile, IDE, and custom clients.

### `GET /api/interaction-endpoints`

Query:

- `limit`: optional, defaults to `100`.

Response:

```json
{
  "items": [
    {
      "id": "iep_...",
      "kind": "web",
      "provider": "browser",
      "displayName": "Web Console",
      "targetPrefix": "#",
      "inboundEnabled": true,
      "outboundEnabled": true,
      "authMode": "cookie",
      "configJson": "{}"
    }
  ]
}
```

### `POST /api/interaction-endpoints`

Request:

```json
{
  "kind": "web",
  "provider": "browser",
  "displayName": "Web Console",
  "targetPrefix": "#",
  "inboundEnabled": true,
  "outboundEnabled": true,
  "authMode": "cookie",
  "configJson": "{}"
}
```

## Messages

### `GET /api/messages?target=%23general`

Query:

- `target`: required target such as `#general`, `#general:thread`, or `dm:user`.
- `limit`: optional, defaults to `50`.

### `POST /api/messages`

Request:

```json
{
  "target": "#general",
  "content": "hello",
  "role": "user",
  "sourceEndpointId": "iep_...",
  "externalMessageId": "optional-upstream-id",
  "metadataJson": "{}",
  "requestId": "optional-idempotency-key"
}
```

## Tasks

Task states are `todo`, `in_progress`, `in_review`, and `done`.

### `GET /api/tasks`

Query:

- `state`: optional.
- `target`: optional.
- `limit`: optional, defaults to `100`.

### `POST /api/tasks`

Request:

```json
{
  "summary": "wire backend",
  "target": "#general",
  "state": "todo",
  "assigneeId": "usr_..."
}
```

### `PATCH /api/tasks/{id}`

Request:

```json
{
  "summary": "updated summary",
  "state": "in_progress",
  "assigneeId": "usr_..."
}
```
