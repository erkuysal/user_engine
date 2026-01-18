# UserEngine Setup Guide

A multi-tenant real-time presence engine for Go applications. Track user online/offline status across workspaces with WebSocket support, automatic session cleanup, and graceful disconnect handling.

---

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [Prerequisites](#prerequisites)
- [Installation Methods](#installation-methods)
  - [Method 1: Standalone Services](#method-1-standalone-services)
  - [Method 2: Embedded Library](#method-2-embedded-library)
  - [Method 3: Remote SDK Client](#method-3-remote-sdk-client)
- [Configuration Reference](#configuration-reference)
- [Authentication Setup](#authentication-setup)
- [API Reference](#api-reference)
- [WebSocket Integration](#websocket-integration)
- [Event Schema](#event-schema)
- [Production Deployment](#production-deployment)
- [Troubleshooting](#troubleshooting)

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        Your Application                          │
├─────────────────────────────────────────────────────────────────┤
│   Option A: Embedded          │   Option B: Remote Client       │
│   ┌─────────────────────┐     │   ┌─────────────────────┐       │
│   │  pkg/presence       │     │   │  pkg/sdk            │       │
│   │  pkg/events         │     │   │  (HTTP/WS client)   │       │
│   │  pkg/ws             │     │   └──────────┬──────────┘       │
│   └──────────┬──────────┘     │              │                  │
│              │                │              │ HTTP/WS          │
│              ▼                │              ▼                  │
│   ┌─────────────────────┐     │   ┌─────────────────────┐       │
│   │      Redis          │     │   │  Standalone Services │       │
│   └─────────────────────┘     │   │  (Gateway + API +    │       │
│                               │   │   Workers)           │       │
│                               │   └──────────┬──────────┘       │
│                               │              │                  │
│                               │              ▼                  │
│                               │   ┌─────────────────────┐       │
│                               │   │      Redis          │       │
│                               │   └─────────────────────┘       │
└─────────────────────────────────────────────────────────────────┘
```

**Choose your integration method:**

| Method | Best For | Latency | Complexity |
|--------|----------|---------|------------|
| **Embedded** | Monoliths, single-binary apps | Lowest | Medium |
| **Standalone** | Microservices, shared presence | Low | Higher |
| **SDK Client** | Apps connecting to standalone | Medium | Lowest |

---

## Prerequisites

- **Go 1.21+**
- **Redis 6.2+** (for Lua scripting support)

---

## Installation Methods

### Method 1: Standalone Services

Deploy UserEngine as separate microservices. Best for sharing presence across multiple applications.

#### Step 1: Clone or Copy the Module

```bash
# Option A: Clone the repository
git clone https://github.com/your-org/userengine.git
cd userengine

# Option B: Copy to your monorepo
cp -r /path/to/UserEngine ./services/presence
```

#### Step 2: Configure Environment

Create `.env` or set environment variables:

```bash
# Required
REDIS_ADDR=localhost:6379
JWT_SECRET=your-secret-key-min-32-chars-long

# Optional
REDIS_PASSWORD=
REDIS_DB=0
GATEWAY_ADDR=:8080
API_ADDR=:8081
SESSION_TTL=60s
HEARTBEAT_INTERVAL=20s
DEBOUNCE_DELAY=5s
SWEEP_INTERVAL=30s
SWEEP_BATCH_SIZE=100
SWEEP_TIME_BUDGET=500ms
```

#### Step 3: Build and Run

**Option A: Using Scripts (Recommended)**

```bash
# Linux/macOS/WSL
./scripts/start.sh        # Start all services
./scripts/status.sh       # Check status
./scripts/stop.sh         # Stop all services

# Windows PowerShell
.\scripts\Start.ps1       # Start all services
.\scripts\Status.ps1      # Check status
.\scripts\Stop.ps1        # Stop all services
```

**Option B: Using Make**

```bash
make build                # Build all services
make run                  # Build and start all
make stop                 # Stop all services
make status               # Check status
make help                 # Show all commands
```

**Option C: Manual**

```bash
# Build all binaries
go build -o bin/gateway ./cmd/gateway
go build -o bin/api ./cmd/api
go build -o bin/sweeper ./cmd/worker-sweeper
go build -o bin/debouncer ./cmd/worker-debouncer

# Run all services
./bin/gateway &
./bin/api &
./bin/sweeper &
./bin/debouncer &
```

#### Step 4: Docker Compose (Recommended)

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    volumes:
      - redis_data:/data

  gateway:
    build:
      context: .
      dockerfile: Dockerfile
      args:
        SERVICE: gateway
    ports:
      - "8080:8080"
    environment:
      - REDIS_ADDR=redis:6379
      - JWT_SECRET=${JWT_SECRET}
    depends_on:
      - redis

  api:
    build:
      context: .
      dockerfile: Dockerfile
      args:
        SERVICE: api
    ports:
      - "8081:8081"
    environment:
      - REDIS_ADDR=redis:6379
      - JWT_SECRET=${JWT_SECRET}
    depends_on:
      - redis

  sweeper:
    build:
      context: .
      dockerfile: Dockerfile
      args:
        SERVICE: worker-sweeper
    environment:
      - REDIS_ADDR=redis:6379
    depends_on:
      - redis

  debouncer:
    build:
      context: .
      dockerfile: Dockerfile
      args:
        SERVICE: worker-debouncer
    environment:
      - REDIS_ADDR=redis:6379
    depends_on:
      - redis

volumes:
  redis_data:
```

Create `Dockerfile`:

```dockerfile
FROM golang:1.21-alpine AS builder
ARG SERVICE
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /service ./cmd/${SERVICE}

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /service /service
COPY --from=builder /app/pkg/presence/lua /lua
ENTRYPOINT ["/service"]
```

```bash
docker-compose up -d
```

---

### Method 2: Embedded Library

Import UserEngine packages directly into your Go application. Best for monoliths or when you want full control.

#### Step 1: Add as Dependency

```bash
# If UserEngine is in a separate repo
go get github.com/your-org/userengine

# If UserEngine is local, use replace directive in go.mod
# replace github.com/userengine/presence => ../UserEngine
```

Or copy the `pkg/` directory to your project:

```bash
cp -r /path/to/UserEngine/pkg ./internal/presence-engine
```

#### Step 2: Initialize in Your Application

```go
package main

import (
    "context"
    "log"
    "net/http"
    "time"

    "github.com/go-redis/redis/v8"
    
    // Import UserEngine packages
    "github.com/userengine/presence/pkg/presence"
    "github.com/userengine/presence/pkg/events"
    "github.com/userengine/presence/pkg/ws"
    "github.com/userengine/presence/pkg/auth"
)

func main() {
    ctx := context.Background()

    // 1. Connect to Redis
    rdb := redis.NewClient(&redis.Options{
        Addr: "localhost:6379",
    })

    // 2. Initialize presence service
    presenceSvc := presence.NewService(rdb)
    presenceSvc.SetSessionTTL(60 * time.Second)
    if err := presenceSvc.LoadScripts(ctx); err != nil {
        log.Fatal(err)
    }

    // 3. Initialize event bus
    eventBus := events.NewRedisPubSub(rdb)

    // 4. Initialize JWT validator
    jwtValidator := auth.NewValidator("your-jwt-secret")

    // 5. Initialize WebSocket hub
    hub := ws.NewHub(presenceSvc, eventBus, jwtValidator)
    go hub.Run(ctx)

    // 6. Start background workers
    sweeper := presence.NewSweeper(rdb, presenceSvc, eventBus)
    go sweeper.RunWorker(ctx) // Runs sweeper loop

    debouncer := presence.NewDebouncer(rdb, presenceSvc, eventBus)
    go debouncer.RunWorker(ctx) // Runs debouncer loop

    // 7. Mount WebSocket handler on your router
    http.HandleFunc("/ws", hub.HandleWebSocket)
    
    // 8. Add your own presence API endpoints
    http.HandleFunc("/api/presence/lookup", func(w http.ResponseWriter, r *http.Request) {
        // Use presenceSvc.LookupUsers() directly
    })

    log.Println("Server starting on :8080")
    http.ListenAndServe(":8080", nil)
}
```

#### Step 3: Use Presence Service Directly

```go
// Connect a user session
result, err := presenceSvc.Connect(ctx, presence.SessionMeta{
    ScopeID:     "workspace-123",
    UserID:      "user-456",
    SessionID:   "session-789",
    ConnectedAt: time.Now(),
})

if result.Transition == "online" {
    // User just came online - publish event
    eventBus.Publish(ctx, "workspace-123", events.PresenceEvent{
        EventID:    events.NewEventID(),
        ScopeID:    "workspace-123",
        Type:       events.EventTypeUserOnline,
        UserID:     "user-456",
        Version:    result.Version,
        OccurredAt: time.Now(),
        Source:     "my-app",
    })
}

// Check if user is online
online, _ := presenceSvc.IsUserOnline(ctx, "workspace-123", "user-456")

// Lookup multiple users
statuses, _ := presenceSvc.LookupUsers(ctx, "workspace-123", []string{"user-1", "user-2", "user-3"})

// Heartbeat (extend session TTL)
presenceSvc.Heartbeat(ctx, "workspace-123", "session-789")

// Disconnect
result, _ = presenceSvc.Disconnect(ctx, "workspace-123", "session-789", "user-456")
if result.Transition == "offline" {
    // User's last session ended - publish offline event
}
```

---

### Method 3: Remote SDK Client

Connect to standalone UserEngine services from any Go application.

#### Step 1: Import SDK

```go
import "github.com/userengine/presence/pkg/sdk"
```

#### Step 2: Create Client

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/userengine/presence/pkg/sdk"
)

func main() {
    // Create client pointing to standalone services
    client := sdk.NewClient(sdk.Config{
        APIURL:     "http://localhost:8081",
        GatewayURL: "ws://localhost:8080",
        JWTToken:   "your-jwt-token",
    })

    ctx := context.Background()

    // Lookup user presence
    statuses, err := client.LookupUsers(ctx, "workspace-123", []string{"alice", "bob"})
    if err != nil {
        log.Fatal(err)
    }
    
    for user, status := range statuses {
        fmt.Printf("%s: %s\n", user, status)
    }

    // Subscribe to real-time events
    eventCh, cancel, err := client.Subscribe(ctx, "workspace-123")
    if err != nil {
        log.Fatal(err)
    }
    defer cancel()

    for event := range eventCh {
        fmt.Printf("Event: %s - User: %s\n", event.Type, event.UserID)
    }
}
```

---

## Configuration Reference

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_ADDR` | `localhost:6379` | Redis server address |
| `REDIS_PASSWORD` | `` | Redis password (optional) |
| `REDIS_DB` | `0` | Redis database number |
| `JWT_SECRET` | `dev-secret...` | Secret for JWT signing/validation |
| `GATEWAY_ADDR` | `:8080` | WebSocket gateway listen address |
| `API_ADDR` | `:8081` | REST API listen address |
| `SESSION_TTL` | `60s` | Session expiration time |
| `HEARTBEAT_INTERVAL` | `20s` | Client heartbeat interval |
| `DEBOUNCE_DELAY` | `5s` | Disconnect debounce window |
| `SWEEP_INTERVAL` | `30s` | Sweeper run interval |
| `SWEEP_BATCH_SIZE` | `100` | Sessions per sweep batch |
| `SWEEP_TIME_BUDGET` | `500ms` | Max time per sweep run |

---

## Authentication Setup

### JWT Token Structure

```json
{
  "user_id": "alice",
  "scopes": ["workspace-1", "workspace-2"],
  "exp": 1735123456
}
```

### Generate Tokens in Your Auth Service

```go
import "github.com/userengine/presence/pkg/auth"

validator := auth.NewValidator("your-jwt-secret")

// Generate token for a user
token, err := validator.GenerateToken("user-123", []string{"workspace-a", "workspace-b"})
```

### Validate Tokens

```go
claims, err := validator.ValidateToken(token)
if err != nil {
    // Invalid token
}

// Check scope authorization
if !validator.HasScope(claims, "workspace-a") {
    // User not authorized for this workspace
}
```

---

## API Reference

### REST Endpoints

#### `POST /presence/lookup`

Lookup presence status for multiple users.

**Request:**
```json
{
  "scope_id": "workspace-123",
  "user_ids": ["alice", "bob", "charlie"]
}
```

**Response:**
```json
{
  "statuses": {
    "alice": "online",
    "bob": "offline",
    "charlie": "online"
  }
}
```

**Headers:**
```
Authorization: Bearer <jwt-token>
Content-Type: application/json
```

#### `GET /presence/:scope_id/online`

Get all online users in a scope.

**Response:**
```json
{
  "users": ["alice", "charlie", "dave"]
}
```

### WebSocket Endpoint

#### `GET /ws?scope_id=<scope>&token=<jwt>&device_id=<device_id>`

Establishes WebSocket connection for real-time presence events.

**Client → Server Messages:**

```json
{"type": "heartbeat"}
```

**Server → Client Messages:**

```json
{
  "event_id": "evt_abc123",
  "scope_id": "workspace-123",
  "type": "user_online",
  "user_id": "alice",
  "version": 5,
  "tab_count": 1,
  "device_id": "device-abc",
  "occurred_at": "2024-01-15T10:30:00Z",
  "source": "gateway"
}
```

---

## Event Schema

### Event Types

| Type | Description |
|------|-------------|
| `user_online` | User's first session connected |
| `user_offline` | User's last session disconnected |
| `user_tabs` | Session count changed while user stays online |

### Event Fields

```go
type PresenceEvent struct {
    EventID    string    `json:"event_id"`    // Unique event ID
    ScopeID    string    `json:"scope_id"`    // Workspace/scope identifier
    Type       string    `json:"type"`        // "user_online", "user_offline", "user_tabs"
    UserID     string    `json:"user_id"`     // User identifier
    Version    int64     `json:"version"`     // Monotonic version for ordering
    TabCount   int64     `json:"tab_count"`   // Active session count
    DeviceID   string    `json:"device_id"`   // Client-provided device identifier
    OccurredAt time.Time `json:"occurred_at"` // Event timestamp
    Source     string    `json:"source"`      // Service that emitted event
}
```

### Event Ordering

Events include a monotonic `version` per user. Clients should:

1. Track `lastVersion` per user
2. Ignore events where `event.Version <= lastVersion`
3. This handles out-of-order delivery and retries

```javascript
// Client-side deduplication
const userVersions = new Map();

function handleEvent(event) {
    const lastVersion = userVersions.get(event.user_id) || 0;
    if (event.version <= lastVersion) {
        return; // Stale event, ignore
    }
    userVersions.set(event.user_id, event.version);
    // Process event...
}
```

---

## Production Deployment

### Scaling

| Component | Scaling Strategy |
|-----------|------------------|
| Gateway | Horizontal (stateless, sticky sessions optional) |
| API | Horizontal (stateless) |
| Sweeper | Single instance per Redis (leader election) |
| Debouncer | Single instance per Redis (leader election) |

### Redis Configuration

```
# redis.conf
maxmemory 1gb
maxmemory-policy volatile-lru
appendonly yes
appendfsync everysec
```

### Health Checks

```bash
# Gateway health
curl http://localhost:8080/health

# API health
curl http://localhost:8081/health

# Redis connectivity
redis-cli ping
```

### Monitoring

Key metrics to track:

- `presence_connections_total` - Total WS connections
- `presence_events_published_total` - Events published
- `presence_sweep_duration_seconds` - Sweep operation time
- `presence_debounce_jobs_pending` - Pending disconnect jobs

---

## Troubleshooting

### Common Issues

**"Redis connection refused"**
```bash
# Check Redis is running
redis-cli ping

# Check connectivity
telnet localhost 6379
```

**"JWT validation failed"**
- Ensure `JWT_SECRET` matches between token generator and UserEngine
- Check token expiration (`exp` claim)
- Verify scope includes the requested workspace

**"User shows as offline immediately"**
- Check `SESSION_TTL` is long enough
- Ensure heartbeats are being sent
- Verify sweeper isn't running too aggressively

**"Events not received on WebSocket"**
- Check Redis Pub/Sub is working: `redis-cli SUBSCRIBE presence:events:*`
- Verify scope_id matches between publisher and subscriber
- Check for WebSocket connection errors in browser console

**"Duplicate events received"**
- This is expected behavior - use `version` field for client-side deduplication
- Implement the ordering logic shown in [Event Ordering](#event-ordering)

### Debug Mode

Enable verbose logging:

```bash
export LOG_LEVEL=debug
./bin/gateway
```

### Redis Key Inspection

```bash
# List all presence keys
redis-cli KEYS "presence:*"

# Check active sessions in a scope
redis-cli SMEMBERS "presence:scope:workspace-123:sessions"

# Check user's sessions
redis-cli SMEMBERS "presence:scope:workspace-123:user:alice:sessions"

# Check online users
redis-cli SMEMBERS "presence:scope:workspace-123:online"

# Check user version
redis-cli GET "presence:scope:workspace-123:user:alice:version"
```

---

## Quick Reference

### Embedded Usage (Copy-Paste)

```go
import (
    "github.com/userengine/presence/pkg/presence"
    "github.com/userengine/presence/pkg/events"
    "github.com/userengine/presence/pkg/ws"
    "github.com/userengine/presence/pkg/auth"
)

// Initialize
rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
svc := presence.NewService(rdb)
svc.LoadScripts(ctx)
eventBus := events.NewRedisPubSub(rdb)
validator := auth.NewValidator("secret")
hub := ws.NewHub(svc, eventBus, validator)

// Run
go hub.Run(ctx)
go presence.NewSweeper(rdb, svc, eventBus).RunWorker(ctx)
go presence.NewDebouncer(rdb, svc, eventBus).RunWorker(ctx)

// Use
http.HandleFunc("/ws", hub.HandleWebSocket)
```

### SDK Usage (Copy-Paste)

```go
import "github.com/userengine/presence/pkg/sdk"

client := sdk.NewClient(sdk.Config{
    APIURL:     "http://localhost:8081",
    GatewayURL: "ws://localhost:8080",
    JWTToken:   "your-token",
})

statuses, _ := client.LookupUsers(ctx, "scope", []string{"user1", "user2"})
```

---

---

## Non-Go Backend Integration

For Django, .NET, or other backends, run UserEngine as standalone services and integrate via HTTP/WebSocket.

| Backend | Guide |
|---------|-------|
| **Django/Python** | [INTEGRATION_DJANGO.md](./INTEGRATION_DJANGO.md) |
| **.NET/C#** | [INTEGRATION_DOTNET.md](./INTEGRATION_DOTNET.md) |

### Key Integration Points

1. **JWT Tokens** - Generate tokens in your auth system using the same secret
2. **REST API** - Use `POST /presence/lookup` for status checks
3. **WebSocket** - Connect frontend directly to UserEngine gateway
4. **Redis Pub/Sub** - Subscribe server-side for real-time events

```
Your Backend                    UserEngine
     │                              │
     ├─── Generate JWT ────────────►│ (same secret)
     │                              │
     ├─── HTTP Lookup ─────────────►│ POST /presence/lookup
     │                              │
     │    Frontend ────────────────►│ WebSocket /ws
     │                              │
     ◄─── Redis Pub/Sub ───────────►│ presence:events:{scope}
```

---

## Support

- **Issues**: [GitHub Issues](https://github.com/your-org/userengine/issues)
- **Docs**: [Full Documentation](https://github.com/your-org/userengine/docs)

