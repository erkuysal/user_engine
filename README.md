# UserEngine - Go Presence Engine

<p align="center">
  <strong>A production-ready, multi-tenant user presence engine for real-time applications</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/Redis-7+-DC382D?style=flat&logo=redis" alt="Redis">
  <img src="https://img.shields.io/badge/License-MIT-green.svg" alt="License">
</p>

---

## ✨ Features

- **🚀 High Performance** - Atomic Redis/Lua operations for consistent state transitions
- **🏢 Multi-Tenant** - Isolated presence tracking per scope/workspace
- **📡 Real-Time** - WebSocket gateway with Pub/Sub event broadcasting
- **🔄 Resilient** - Debounced disconnects prevent flicker, sweeper cleans zombies
- **📊 Observable** - Prometheus metrics, structured logging, health checks
- **🔐 Secure** - JWT authentication, CORS configuration, webhook signatures
- **🎭 Advanced** - Invisible/ghost mode, device-aware presence
- **📦 Flexible** - Embed as library or deploy as microservices
- **🔧 Configurable** - `.env` file support, environment-specific configs

## 📋 Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [Usage Modes](#usage-modes)
- [Configuration](#configuration)
- [API Reference](#api-reference)
- [Advanced Features](#advanced-features)
- [Observability](#observability)
- [Architecture](#architecture)
- [Testing](#testing)
- [License](#license)

## Installation

```bash
go get github.com/userengine/presence
```

## Quick Start

### Using Docker Compose

```bash
# Start all services
docker-compose up -d

# Generate a test token
go run ./cmd/api -generate-token user123 workspace-1

# Connect via WebSocket
wscat -c "ws://localhost:8080/ws?scope_id=workspace-1&token=<token>"
```

### Manual Setup

```bash
# 1. Start Redis
docker run -d --name redis -p 6379:6379 redis:7-alpine

# 2. Copy and configure environment
cp env.example .env
# Edit .env with your settings

# 3. Run services
go run ./cmd/gateway          # WebSocket gateway (:8080)
go run ./cmd/api              # REST API (:8081)
go run ./cmd/worker-sweeper   # Cleanup worker
go run ./cmd/worker-debouncer # Disconnect debouncer
go run ./cmd/worker-webhook   # Webhook delivery (optional)
```

## Usage Modes

### Mode 1: Embedded (Library)

Embed presence directly in your Go application:

```go
package main

import (
    "context"
    "github.com/userengine/presence/pkg/presence"
    "github.com/userengine/presence/pkg/events"
    "github.com/go-redis/redis/v8"
)

func main() {
    ctx := context.Background()
    rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
    
    // Initialize service
    svc := presence.NewService(rdb)
    svc.LoadScripts(ctx)
    
    // Start background workers
    eventBus := events.NewRedisPubSub(rdb)
    
    sweeper := presence.NewSweeper(svc, eventBus, presence.DefaultSweeperConfig())
    go sweeper.RunWorker(ctx)
    
    debouncer := presence.NewDebouncer(svc, eventBus, presence.DefaultDebouncerConfig())
    go debouncer.RunWorker(ctx)
    
    // Connect a user
    result, _ := svc.Connect(ctx, presence.SessionMeta{
        ScopeID:    "workspace-1",
        UserID:     "user-123",
        SessionID:  "session-abc",
        DeviceType: "desktop",
    })
    
    // Check presence
    statuses, _ := svc.LookupUsers(ctx, "workspace-1", []string{"user-1", "user-2"})
}
```

See [`examples/embedded/`](examples/embedded/) for a complete example.

### Mode 2: Standalone Services

Deploy as independent microservices:

```bash
# Using make
make build
./bin/gateway &
./bin/api &
./bin/worker-sweeper &
./bin/worker-debouncer &

# Or with Docker Compose
docker-compose up -d
```

Connect using the SDK:

```go
import "github.com/userengine/presence/pkg/sdk"

// Create client with auto-reconnect
client := sdk.NewClient(
    "http://localhost:8081",
    "ws://localhost:8080",
    token,
    sdk.WithReconnectConfig(sdk.DefaultReconnectConfig()),
)

// REST API calls
statuses, _ := client.Lookup(ctx, "workspace-1", []string{"user-1", "user-2"})
online, _ := client.IsOnline(ctx, "workspace-1", "user-1")
users, _ := client.GetAllOnline(ctx, "workspace-1")

// Real-time WebSocket connection
conn, _ := client.Connect(ctx, "workspace-1",
    sdk.WithDeviceID("browser-123"),
    sdk.WithAutoReconnect(true),
)

conn.OnEvent = func(e events.PresenceEvent) {
    fmt.Printf("%s is now %s (version: %d)\n", e.UserID, e.Type, e.Version)
}

conn.OnStateChange = func(state sdk.ConnectionState) {
    fmt.Printf("Connection state: %s\n", state)
}

// Wait for events
<-conn.Done()
```

See [`examples/sdk-client/`](examples/sdk-client/) for a complete example.

## Configuration

### Environment Variables

UserEngine loads configuration from environment variables and `.env` files.

#### File Loading Order (later overrides earlier)

1. `.env` - Base configuration
2. `.env.local` - Local overrides (gitignored)
3. `.env.{ENVIRONMENT}` - Environment-specific (e.g., `.env.production`)
4. `.env.{ENVIRONMENT}.local` - Local env-specific overrides
5. **Shell environment variables** - Always win

#### Core Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `ENVIRONMENT` | `development` | Environment name (`development`, `staging`, `production`) |
| `REDIS_ADDR` | `localhost:6379` | Redis server address |
| `REDIS_PASSWORD` | `` | Redis password |
| `REDIS_DB` | `0` | Redis database number |
| `GATEWAY_ADDR` | `:8080` | WebSocket gateway listen address |
| `API_ADDR` | `:8081` | REST API listen address |
| `JWT_SECRET` | `dev-secret-...` | JWT signing secret (**change in production!**) |

#### Session & Timing

| Variable | Default | Description |
|----------|---------|-------------|
| `SESSION_TTL` | `45s` | Session TTL in Redis |
| `HEARTBEAT_INTERVAL` | `15s` | Expected heartbeat interval |
| `HEARTBEAT_RATE_LIMIT` | `5s` | Minimum time between heartbeats |
| `DISCONNECT_DEBOUNCE_DELAY` | `5s` | Grace period before offline |
| `PING_PERIOD` | `54s` | WebSocket ping interval |
| `PONG_WAIT` | `60s` | WebSocket pong timeout |

#### CORS Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `CORS_ALLOWED_ORIGINS` | `` | Comma-separated allowed origins |
| `CORS_ALLOW_ALL` | `true` (dev) | Allow all origins (disable in production) |

#### Redis Cluster

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_CLUSTER_ENABLED` | `false` | Enable Redis Cluster mode |
| `REDIS_CLUSTER_ADDRS` | `` | Comma-separated cluster node addresses |

#### Webhooks

| Variable | Default | Description |
|----------|---------|-------------|
| `WEBHOOK_ENABLED` | `false` | Enable async webhook delivery |
| `WEBHOOK_STREAM_NAME` | `presence:webhooks` | Redis Stream name |
| `WEBHOOK_SECRET` | `` | HMAC-SHA256 signing secret |
| `WEBHOOK_RETRY_ATTEMPTS` | `3` | Max delivery retries |
| `WEBHOOK_TIMEOUT` | `10s` | HTTP request timeout |

#### Flapping Protection

| Variable | Default | Description |
|----------|---------|-------------|
| `FLAPPING_WINDOW` | `30s` | Time window for transition tracking |
| `FLAPPING_THRESHOLD` | `5` | Max transitions before suppressing |
| `OFFLINE_DELAY` | `15s` | Sustained offline before broadcasting |

#### Workers

| Variable | Default | Description |
|----------|---------|-------------|
| `SWEEPER_INTERVAL` | `30s` | Time between sweep cycles |
| `SWEEPER_BUDGET_PER_SCOPE` | `100ms` | Max time per scope per sweep |

### Example `.env` File

```env
# Environment
ENVIRONMENT=production

# Security (REQUIRED for production)
JWT_SECRET=your-super-secret-jwt-key-minimum-32-chars

# Redis
REDIS_ADDR=redis.example.com:6379
REDIS_PASSWORD=your-redis-password

# CORS (REQUIRED for production)
CORS_ALLOW_ALL=false
CORS_ALLOWED_ORIGINS=https://app.example.com,https://admin.example.com

# Webhooks (optional)
WEBHOOK_ENABLED=true
WEBHOOK_SECRET=your-webhook-signing-secret
```

## API Reference

### REST Endpoints

#### POST /presence/lookup

Look up presence status for specific users.

```bash
curl -X POST http://localhost:8081/presence/lookup \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"scope_id": "workspace-1", "user_ids": ["user1", "user2"]}'
```

**Response:**
```json
{
  "statuses": {
    "user1": "online",
    "user2": "offline"
  }
}
```

#### GET /presence/online

Get all online users in a scope (paginated).

```bash
curl "http://localhost:8081/presence/online?scope_id=workspace-1&cursor=0&limit=100" \
  -H "Authorization: Bearer <token>"
```

**Response:**
```json
{
  "users": ["user1", "user3", "user5"],
  "cursor": "42"
}
```

#### POST /presence/disconnect_user

Force disconnect a user (server-side logout).

```bash
curl -X POST http://localhost:8081/presence/disconnect_user \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"scope_id": "workspace-1", "user_id": "user1"}'
```

**Response:**
```json
{
  "transition": "offline",
  "version": 43,
  "session_count": 0
}
```

#### POST /presence/devices

Get device breakdown for users.

```bash
curl -X POST http://localhost:8081/presence/devices \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"scope_id": "workspace-1", "user_ids": ["user1", "user2"]}'
```

**Response:**
```json
{
  "devices": {
    "user1": {"desktop": 1, "mobile": 2},
    "user2": {"desktop": 1}
  }
}
```

### WebSocket API

#### Connection

```
ws://localhost:8080/ws?scope_id=<scope>&token=<jwt>&device_id=<device>&invisible=<bool>
```

**Query Parameters:**
| Parameter | Required | Description |
|-----------|----------|-------------|
| `scope_id` | Yes | Workspace/tenant ID |
| `token` | Yes | JWT authentication token |
| `device_id` | No | Stable device identifier |
| `device_type` | No | Device type (`desktop`, `mobile`, `tablet`) |
| `invisible` | No | Connect in ghost mode (`true`/`false`) |

#### Client Messages

**Heartbeat:**
```json
{"type": "heartbeat"}
```

#### Server Messages

**Presence Event:**
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

**Reconnect Required:**
```json
{
  "type": "reconnect_required",
  "reason": "session_expired"
}
```

#### Event Types

| Type | Description | `tab_count` |
|------|-------------|-------------|
| `user_online` | User's first session connected | 1 |
| `user_offline` | User's last session disconnected | 0 |
| `user_tabs` | Session count changed while online | 2+ |

### Health Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Full health check with component status |
| `GET /livez` | Kubernetes liveness probe |
| `GET /readyz` | Kubernetes readiness probe |
| `GET /metrics` | Prometheus metrics |
| `GET /metrics/json` | JSON metrics snapshot |

## Advanced Features

### Invisible/Ghost Mode

Allow users to receive events while appearing offline to others:

```go
// SDK connection
conn, _ := client.Connect(ctx, scopeID,
    sdk.WithInvisible(true),
)

// WebSocket URL
ws://localhost:8080/ws?scope_id=workspace-1&token=<jwt>&invisible=true
```

Invisible users:
- Receive all presence events normally
- Are excluded from `LookupUsers` and online lists
- Can be retrieved via admin endpoints

### Device-Aware Presence

Track presence by device type:

```go
// Connect with device info
result, _ := svc.Connect(ctx, presence.SessionMeta{
    ScopeID:    "workspace-1",
    UserID:     "user-123",
    SessionID:  "session-abc",
    DeviceType: "mobile",  // "desktop", "mobile", "tablet", etc.
})

// Get device breakdown
devices, _ := svc.GetUserDevices(ctx, "workspace-1", "user-123")
// Returns: {"desktop": 1, "mobile": 2}
```

### Webhook Integration

Receive presence events via HTTP webhooks:

```env
WEBHOOK_ENABLED=true
WEBHOOK_SECRET=your-signing-secret
```

Webhook payload:
```json
{
  "id": "event-uuid",
  "event": {
    "event_id": "...",
    "type": "user_online",
    "user_id": "user123",
    ...
  },
  "webhook_url": "https://your-app.com/webhook",
  "created_at": "2024-01-15T10:30:00Z",
  "attempt": 0,
  "signature": "hmac-sha256-signature"
}
```

Verify signature:
```go
expectedSig := hmac.New(sha256.New, []byte(webhookSecret))
expectedSig.Write(eventPayload)
valid := hmac.Equal([]byte(signature), expectedSig.Sum(nil))
```

### Redis Cluster Support

For horizontal scalability:

```env
REDIS_CLUSTER_ENABLED=true
REDIS_CLUSTER_ADDRS=node1:7000,node2:7001,node3:7002
```

Keys use hash tags (`{scope_id}`) to ensure related data lands on the same shard.

### Flapping Protection

Prevents rapid online/offline "flicker" during unstable connections:

```env
FLAPPING_WINDOW=30s      # Track transitions over 30 seconds
FLAPPING_THRESHOLD=5     # Suppress if >5 transitions in window
OFFLINE_DELAY=15s        # Wait 15s of sustained offline before broadcasting
```

## Observability

### Prometheus Metrics

Available at `GET /metrics`:

```
# Connections
presence_connections_active{scope="workspace-1"} 42
presence_connections_total 1234

# Events
presence_events_published_total{type="user_online"} 5678
presence_events_consumed_total{type="user_online"} 5670
presence_events_dropped_total 8

# Redis
presence_redis_operations_total{operation="GET"} 12345
presence_redis_errors_total 2
presence_redis_latency_seconds{quantile="0.99"} 0.005

# Workers
presence_sweeper_ticks_total 100
presence_sweeper_sessions_pruned_total 50
presence_debouncer_jobs_scheduled_total 200
presence_debouncer_jobs_executed_total 180
presence_debouncer_jobs_cancelled_total 20
```

### Structured Logging

Uses `zerolog` with JSON output:

```json
{
  "level": "info",
  "time": "2024-01-15T10:30:00Z",
  "scope_id": "workspace-1",
  "user_id": "user123",
  "session_id": "session-abc",
  "transition": "online",
  "version": 42,
  "message": "user connected"
}
```

### Health Checks

```bash
# Full health check
curl http://localhost:8081/health

# Response
{
  "status": "healthy",
  "version": "1.0.0",
  "checks": [
    {"name": "redis", "status": "healthy", "latency": 1234567},
    {"name": "lua_scripts", "status": "healthy", "message": "all scripts loaded"}
  ]
}
```

## Architecture

### Package Structure

```
UserEngine/
├── cmd/
│   ├── gateway/           # WebSocket gateway service
│   ├── api/               # REST API service
│   ├── worker-sweeper/    # Zombie session cleanup
│   ├── worker-debouncer/  # Disconnect delay processing
│   └── worker-webhook/    # Async webhook delivery
├── pkg/
│   ├── auth/              # JWT validation & middleware
│   ├── config/            # Configuration loading (.env support)
│   ├── events/            # Event schema & Redis Pub/Sub
│   ├── health/            # Health check handlers
│   ├── metrics/           # Prometheus metrics
│   ├── presence/          # Core logic (Service, Sweeper, Debouncer, Keys)
│   ├── redis/             # Redis client factory (standalone/cluster)
│   ├── sdk/               # Go client SDK with auto-reconnect
│   ├── webhooks/          # Webhook producer & worker
│   └── ws/                # WebSocket hub & client handling
├── test/                  # Unit & integration tests
├── examples/
│   ├── embedded/          # Library embedding example
│   └── sdk-client/        # SDK usage example
├── docker-compose.yml     # Full stack deployment
├── Makefile               # Build & run commands
└── env.example            # Configuration template
```

### Data Flow

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Client    │────▶│   Gateway   │────▶│    Redis    │
│ (WebSocket) │◀────│  (ws/hub)   │◀────│  (Pub/Sub)  │
└─────────────┘     └─────────────┘     └─────────────┘
                           │                    │
                           ▼                    ▼
                    ┌─────────────┐     ┌─────────────┐
                    │   Presence  │────▶│  Lua Scripts│
                    │   Service   │◀────│  (Atomic)   │
                    └─────────────┘     └─────────────┘
                           │
            ┌──────────────┼──────────────┐
            ▼              ▼              ▼
     ┌───────────┐  ┌───────────┐  ┌───────────┐
     │  Sweeper  │  │ Debouncer │  │  Webhook  │
     │  Worker   │  │  Worker   │  │  Worker   │
     └───────────┘  └───────────┘  └───────────┘
```

### Event Ordering & Deduplication

Events use monotonic versioning per user. Clients **must**:

1. Track `lastSeenVersion` per user
2. Only apply events where `version > lastSeenVersion[userID]`
3. Tolerate duplicates (sweeper and debouncer may emit same event)
4. Periodically re-fetch snapshot for consistency

```go
conn.OnEvent = func(e events.PresenceEvent) {
    if e.Version > lastSeen[e.UserID] {
        lastSeen[e.UserID] = e.Version
        applyEvent(e)
    }
}
```

### Failure Modes

| Scenario | Impact | Recovery |
|----------|--------|----------|
| Gateway crash | Clients disconnect | Auto-reconnect with SDK |
| Sweeper delayed | Zombie sessions persist | Cleans up when resumed |
| Debouncer delayed | Late offline events | Eventually consistent |
| Redis failover | Temporary outage | Auto-reconnect, re-snapshot |
| Network flap | Rapid on/off events | Flapping protection suppresses |

## Testing

```bash
# Run all tests
go test ./test/... -v

# Run with race detection
go test ./test/... -race

# Run specific test file
go test ./test/auth_test.go -v

# Integration tests (requires Redis)
REDIS_ADDR=localhost:6379 go test ./test/... -tags=integration

# Manual testing
go run ./test/demo.go
go run ./test/interactive.go
```

### Test Coverage

| Package | Coverage |
|---------|----------|
| `auth` | Token generation, validation, middleware |
| `config` | Loading, validation, env vars |
| `events` | Schema, JSON serialization |
| `health` | Handlers, status computation |
| `metrics` | Counters, gauges, snapshots |
| `presence` | Keys, debouncer config |
| `redis` | Client factory, cluster detection |
| `sdk` | Client, reconnect, connection states |
| `webhooks` | Event serialization |

## Backend Integration

### Token Generation (Node.js)

```javascript
const jwt = require('jsonwebtoken');

function generatePresenceToken(userId, scopes) {
  return jwt.sign(
    { user_id: userId, scopes: scopes },
    process.env.JWT_SECRET,
    { expiresIn: '1h' }
  );
}

// Example: Generate token for a user
const token = generatePresenceToken('user123', ['workspace-1', 'workspace-2']);
```

### Token Generation (Python)

```python
import jwt
from datetime import datetime, timedelta

def generate_presence_token(user_id: str, scopes: list[str]) -> str:
    payload = {
        'user_id': user_id,
        'scopes': scopes,
        'exp': datetime.utcnow() + timedelta(hours=1)
    }
    return jwt.encode(payload, os.environ['JWT_SECRET'], algorithm='HS256')
```

### Proxy Endpoints

Your backend should expose:

```
POST /api/presence/token
  -> Returns { token, wsUrl, scopeId }

GET /api/presence/online?scope_id=...
  -> Proxies to UserEngine API

POST /api/presence/lookup
  -> Proxies to UserEngine API
```

## License

MIT License - See [LICENSE](LICENSE) for details.

---

<p align="center">
  Built with ❤️ for real-time applications
</p>
