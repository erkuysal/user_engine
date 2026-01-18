# UserEngine - Go Presence Engine

A multi-tenant user presence engine implementing atomic Redis/Lua transitions, heartbeat TTL, debounced disconnects, and a scalable sweeper.

**Supports both embedded (library) and standalone (microservice) deployment modes.**

## Installation

```bash
go get github.com/userengine/presence
```

## Usage Modes

### Mode 1: Embedded (Library)

Embed presence directly in your Go application:

```go
import (
    "github.com/userengine/presence/pkg/presence"
    "github.com/userengine/presence/pkg/events"
    "github.com/go-redis/redis/v8"
)

func main() {
    rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
    
    // Initialize service
    svc := presence.NewService(rdb)
    svc.LoadScripts(ctx)
    
    // Start workers as goroutines
    eventBus := events.NewRedisPubSub(rdb)
    
    sweeper := presence.NewSweeper(svc, eventBus, presence.DefaultSweeperConfig())
    go sweeper.RunWorker(ctx)
    
    debouncer := presence.NewDebouncer(svc, eventBus, presence.DefaultDebouncerConfig())
    go debouncer.RunWorker(ctx)
    
    // Use directly in your handlers
    result, _ := svc.Connect(ctx, presence.SessionMeta{
        ScopeID:   "workspace-1",
        UserID:    "user-123",
        SessionID: "session-abc",
    })
    
    statuses, _ := svc.LookupUsers(ctx, "workspace-1", []string{"user-1", "user-2"})
}
```

See [`examples/embedded/`](examples/embedded/) for a complete example.

### Mode 2: Standalone Services

Run as separate microservices:

```bash
# Start Redis
docker run -d --name redis -p 6379:6379 redis:7-alpine

# Run services
go run ./cmd/gateway          # WebSocket gateway (:8080)
go run ./cmd/api              # REST API (:8081)
go run ./cmd/worker-sweeper   # Sweeper worker
go run ./cmd/worker-debouncer # Debouncer worker
```

Connect using the SDK:

```go
import "github.com/userengine/presence/pkg/sdk"

client := sdk.NewClient("http://localhost:8081", "http://localhost:8080", token)

// REST API
statuses, _ := client.Lookup(ctx, "workspace-1", []string{"user-1", "user-2"})

// WebSocket for real-time events
conn, _ := client.Connect(ctx, "workspace-1")
for event := range conn.Events() {
    fmt.Printf("%s is now %s\n", event.UserID, event.Type)
}
```

See [`examples/sdk-client/`](examples/sdk-client/) for a complete example.

## Package Structure

```
pkg/
├── presence/      # Core presence logic (Service, Sweeper, Debouncer)
├── events/        # Event schema and Pub/Sub interfaces
├── auth/          # JWT validation
├── config/        # Configuration helpers
├── ws/            # WebSocket hub and client handling
└── sdk/           # Remote client SDK

cmd/
├── gateway/           # Standalone WebSocket gateway
├── api/               # Standalone REST API
├── worker-sweeper/    # Standalone sweeper worker
└── worker-debouncer/  # Standalone debouncer worker

examples/
├── embedded/      # Embedding as a library
└── sdk-client/    # Using the SDK remotely
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_ADDR` | `localhost:6379` | Redis connection address |
| `REDIS_PASSWORD` | `` | Redis password |
| `GATEWAY_ADDR` | `:8080` | Gateway listen address |
| `API_ADDR` | `:8081` | API listen address |
| `JWT_SECRET` | `dev-secret-...` | JWT signing secret |
| `SESSION_TTL` | `45s` | Session TTL |
| `HEARTBEAT_RATE_LIMIT` | `5s` | Min time between heartbeats |
| `DISCONNECT_DEBOUNCE_DELAY` | `5s` | Delay before disconnect |
| `SWEEPER_INTERVAL` | `30s` | Time between sweeps |
| `SWEEPER_BUDGET_PER_SCOPE` | `100ms` | Max time per scope per sweep |

## Backend Configuration Guide

This section shows a minimal setup for a backend service that issues WebSocket
tokens and proxies presence APIs. The backend must sign JWTs with the same
`JWT_SECRET` used by UserEngine, and include `user_id` and `scopes` claims.

### Required environment variables

```
USERENGINE_API_URL=http://localhost:8081
USERENGINE_WS_URL=ws://localhost:8080
USERENGINE_SCOPE_ID=workspace-1
USERENGINE_TOKEN=<service-jwt-with-scope-access>
JWT_SECRET=dev-secret-change-in-production
```

### Token generation example (Node.js)

```js
const jwt = require('jsonwebtoken');

function generatePresenceToken(userId) {
  return jwt.sign(
    { user_id: userId, scopes: ['workspace-1'] },
    process.env.JWT_SECRET,
    { expiresIn: '1h' }
  );
}
```

### Backend endpoints (example)

```
POST /userengine/token
-> returns { token, wsUrl, scopeId }

GET /presence/online?scope_id=workspace-1
POST /presence/lookup { scope_id, user_ids }
```

### Operational notes

- On logout, close the WebSocket with a normal close code (1000) so the user
  transitions offline immediately.
- Run `worker-sweeper` and `worker-debouncer` in production to clean up stale
  sessions if a client disconnects unexpectedly.

## API Reference

### REST Endpoints

#### POST /presence/lookup (Primary)

```bash
curl -X POST http://localhost:8081/presence/lookup \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"scope_id": "workspace-1", "user_ids": ["user1", "user2"]}'
```

Response:
```json
{"statuses": {"user1": "online", "user2": "offline"}}
```

#### GET /presence/online (Admin)

```bash
curl "http://localhost:8081/presence/online?scope_id=workspace-1" \
  -H "Authorization: Bearer <token>"
```

Response:
```json
{"users": ["user1", "user3"], "cursor": "0"}
```

#### POST /presence/disconnect_user (Server-side logout)

```bash
curl -X POST http://localhost:8081/presence/disconnect_user \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"scope_id": "workspace-1", "user_id": "user1"}'
```

Response:
```json
{"transition": "offline", "version": 43, "session_count": 0}
```

### WebSocket

Connect: `ws://localhost:8080/ws?scope_id=<scope>&token=<jwt>&device_id=<device_id>`

Send heartbeats: `{"type": "heartbeat"}`

Receive events:
```json
{
  "event_id": "uuid",
  "scope_id": "workspace-1",
  "type": "user_online",
  "user_id": "user123",
  "version": 42,
  "tab_count": 1,
  "device_id": "device-abc",
  "occurred_at": "2024-01-15T10:30:00Z"
}
```

### Go SDK

```go
client := sdk.NewClient(apiURL, wsURL, token)

// Lookup
statuses, _ := client.Lookup(ctx, scopeID, userIDs)

// Check single user
online, _ := client.IsOnline(ctx, scopeID, userID)

// Get all online
users, _ := client.GetAllOnline(ctx, scopeID)

// Real-time events
conn, _ := client.Connect(ctx, scopeID)
conn.OnEvent = func(e events.PresenceEvent) { ... }
```

### Embedded Service

```go
svc := presence.NewService(redisClient)
svc.LoadScripts(ctx)

// Connect user
result, _ := svc.Connect(ctx, presence.SessionMeta{...})

// Heartbeat
svc.Heartbeat(ctx, scopeID, sessionID)

// Disconnect
svc.Disconnect(ctx, scopeID, sessionID, userID)

// Lookup
statuses, _ := svc.LookupUsers(ctx, scopeID, userIDs)
online, _ := svc.IsUserOnline(ctx, scopeID, userID)
```

## Event Schema

```json
{
  "event_id": "550e8400-e29b-41d4-a716-446655440000",
  "scope_id": "workspace-1",
  "type": "user_online",
  "user_id": "user123",
  "version": 42,
  "tab_count": 1,
  "device_id": "device-abc",
  "occurred_at": "2024-01-15T10:30:00Z",
  "source": "gateway"
}
```

Event types:
- `user_online`: first session connected (tab_count = 1)
- `user_offline`: last session disconnected (tab_count = 0)
- `user_tabs`: session count changed while user stays online

**Important**: Use `version` for deduplication. Only apply events where `version > lastSeenVersion[userID]`.

## Event Ordering & Deduplication

Ordering is **best-effort**. Multiple publishers emit events concurrently.

Clients must:
1. Track `lastSeenVersion` per user
2. Only apply events where `version > lastSeenVersion[userID]`
3. Tolerate duplicates (sweeper and debouncer may emit same event)
4. Periodically re-fetch snapshot for consistency

## Failure Modes

| Scenario | Impact | Recovery |
|----------|--------|----------|
| Gateway crash | Clients disconnect | Reconnect + re-snapshot |
| Sweeper delayed | Zombie sessions persist | Cleans up when resumed |
| Debouncer delayed | Late offline events | Eventually consistent |
| Redis failover | Temporary outage | Auto-reconnect |

## Testing

```bash
# Unit tests
go test ./pkg/...

# Integration tests (requires Redis)
go test -tags=integration ./pkg/presence/...
```

## License

MIT
