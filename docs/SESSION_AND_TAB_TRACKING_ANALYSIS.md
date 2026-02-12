# UserEngine Session & Tab Tracking Analysis

**Date:** January 27, 2026  
**Component:** UserEngine - Session Management & Tab Tracking  

---

## Executive Summary

UserEngine implements **session-based presence tracking** with **automatic tab/window awareness**. Each browser tab/window establishes its own WebSocket session, and the system tracks:

- **Session IDs**: Unique identifier per connection (UUID)
- **Tab Count**: Number of active sessions per user
- **Device Tracking**: Optional device identification
- **Online/Offline Transitions**: Based on session count
- **Debounce Logic**: Prevents flapping on quick reconnects

---

## Architecture Overview

### Three-Tier Session Model

```
User Level (alice)
├─ Session 1 (Tab 1 - Desktop)   [session-uuid-1]
├─ Session 2 (Tab 2 - Desktop)   [session-uuid-2]
└─ Session 3 (Mobile App)        [session-uuid-3]
```

**Key Concept**: A user is "online" if they have ≥1 active session. When sessions come/go, events are emitted:

- First session → `user_online` event
- Last session → `user_offline` event  
- Sessions added/removed (but ≥1 remains) → `user_tabs` event

---

## Session Lifecycle

### 1. Session Creation (Connect)

**Location**: [`hub.go:216-225`](d:\DEV\UNDER_DEVELOPMENT\personal\BACKENDs\userengine\pkg\ws\hub.go#L216-L225)

```go
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
    // Validate JWT
    claims, err := h.validator.ValidateRequest(r)
    
    // Get parameters
    scopeID := r.URL.Query().Get("scope_id")       // Workspace/org ID
    deviceID := r.URL.Query().Get("device_id")     // Optional device ID
    deviceType := r.URL.Query().Get("device_type") // "desktop", "mobile", "tablet"
    invisible := r.URL.Query().Get("invisible") == "true"
    
    // Create unique session ID
    sessionID := uuid.New().String()
    
    // Create client
    client := NewClient(h, conn, claims.UserID, sessionID, scopeID, deviceID, deviceType, invisible, h.cfg)
    
    // Register and start pumps
    h.register <- client
    go client.WritePump()
    go client.ReadPump()
}
```

**Session Metadata Stored**:
```go
type SessionMeta struct {
    UserID      string    // "alice"
    SessionID   string    // "uuid-generated"
    ScopeID     string    // "workspace-123"
    ConnectedAt time.Time
    UserAgent   string    // Optional
    IPAddress   string    // Optional
    DeviceID    string    // Optional (client-provided)
    DeviceType  string    // "desktop", "mobile", "tablet"
    Invisible   bool      // Ghost mode
}
```

**Redis Data Structure**:
```
Key: session:{scope_id}:{session_id}
Type: String (JSON)
TTL: 60 seconds (default)
Value: SessionMeta JSON

Key: user_sessions:{scope_id}:{user_id}
Type: Set
Members: [session-1, session-2, session-3]

Key: online_users:{scope_id}
Type: Set
Members: [alice, bob, charlie]

Key: user_version:{scope_id}:{user_id}
Type: String (integer counter)
```

### 2. Heartbeat (Keep-Alive)

**Location**: [`client.go:83-90`](d:\DEV\UNDER_DEVELOPMENT\personal\BACKENDs\userengine\pkg\ws\client.go#L83-L90)

**Mechanism**:
- Client sends `{"type": "heartbeat"}` every 20 seconds (configurable)
- Server extends session TTL by 60 seconds
- If no heartbeat received within TTL, session expires (swept by worker)

**Lua Script** (`heartbeat.lua`):
```lua
-- Extend session TTL
redis.call('EXPIRE', session_key, ttl)
return 1
```

### 3. Session Termination (Disconnect)

**Two Disconnect Modes**:

#### A. Normal Close (Logout / Tab Close)
**Location**: [`client.go:102-107`](d:\DEV\UNDER_DEVELOPMENT\personal\BACKENDs\userengine\pkg\ws\client.go#L102-L107)

```go
isNormal := false
if ce, ok := err.(*websocket.CloseError); ok {
    if ce.Code == 1000 || ce.Code == websocket.CloseNormalClosure {
        isNormal = true
        c.mu.Lock()
        c.normalClose = true
        c.mu.Unlock()
    }
}
```

**Behavior**: **Immediate disconnect** (no debounce)
- Used when user explicitly logs out or closes browser tab properly
- Session removed from Redis immediately
- Events published immediately

#### B. Abnormal Close (Network Issue / Crash)
**Location**: [`client.go:194-201`](d:\DEV\UNDER_DEVELOPMENT\personal\BACKENDs\userengine\pkg\ws\client.go#L194-L201)

```go
// Abnormal close: schedule debounce
debouncer := c.hub.Debouncer()
_, err := debouncer.ScheduleDisconnect(ctx, c.scopeID, c.userID, c.sessionID)
```

**Behavior**: **Delayed disconnect with debounce window** (default: 5 seconds)
- Allows client to reconnect without triggering offline event
- If client reconnects within debounce window, cancel scheduled disconnect
- If debounce expires, session is removed and events published

**Why Debounce?**
Prevents "flapping" (rapid online→offline→online) due to:
- Temporary network issues
- Browser tab switching
- Page navigation
- Mobile network handoff

---

## Tab Counting & Events

### Event Types

| Event | Condition | Example |
|-------|-----------|---------|
| `user_online` | Session count: 0 → 1 | User opens first tab |
| `user_offline` | Session count: 1 → 0 | User closes last tab |
| `user_tabs` | Session count changes (but ≥1) | User opens/closes tabs |

### Event Payload

```go
type PresenceEvent struct {
    EventID    string    `json:"event_id"`    // "evt_abc123"
    ScopeID    string    `json:"scope_id"`    // "workspace-123"
    Type       string    `json:"type"`        // "user_online", "user_offline", "user_tabs"
    UserID     string    `json:"user_id"`     // "alice"
    Version    int64     `json:"version"`     // Monotonic counter (1, 2, 3...)
    TabCount   int64     `json:"tab_count"`   // Current session count (1, 2, 3...)
    DeviceID   string    `json:"device_id"`   // Optional device identifier
    OccurredAt time.Time `json:"occurred_at"` // ISO 8601 timestamp
    Source     string    `json:"source"`      // "gateway", "sweeper", "debouncer"
}
```

### Example Flow

**Scenario**: Alice opens 3 tabs, then closes them

```
1. Alice opens Tab 1 (first session)
   └─ Event: user_online {version: 1, tab_count: 1}

2. Alice opens Tab 2
   └─ Event: user_tabs {version: 2, tab_count: 2}

3. Alice opens Tab 3
   └─ Event: user_tabs {version: 3, tab_count: 3}

4. Alice closes Tab 2
   └─ Event: user_tabs {version: 4, tab_count: 2}

5. Alice closes Tab 1
   └─ Event: user_tabs {version: 5, tab_count: 1}

6. Alice closes Tab 3 (last session)
   └─ Event: user_offline {version: 6, tab_count: 0}
```

---

## Frontend Integration

### Current Implementation

**Location**: [`UserEngineClient.ts`](d:\DEV\UNDER_DEVELOPMENT\personal\frontend\shared-api\src\userengine\UserEngineClient.ts)

**Session Management**:
```typescript
class UserEngineClient {
    private socket: WebSocket | null = null
    private state: UserEngineState = 'disconnected'
    
    public async connect() {
        // 1. Get JWT token from Django
        const response = await http.post('/userengine/token/')
        const { token, wsUrl, scopeId } = response.data
        
        // 2. Connect to WebSocket
        const url = `${wsUrl}/ws?scope_id=${scopeId}&token=${token}`
        this.socket = new WebSocket(url)
        
        // Each tab/window creates its own WebSocket = unique session
    }
    
    public disconnect() {
        this.shouldReconnect = false
        if (this.socket) {
            this.socket.close() // Sends close code 1000 (normal close)
        }
    }
}
```

**Key Characteristics**:
- ✅ Each browser tab creates a **separate WebSocket connection**
- ✅ Each connection gets a **unique session ID** (server-generated)
- ✅ Connections persist across page navigation (singleton pattern)
- ✅ Disconnect only on logout (explicit)

### Tab/Window Behavior

| Scenario | Sessions Created | Result |
|----------|------------------|--------|
| Open 1 tab | 1 session | user_online |
| Open 3 tabs | 3 sessions | user_online + 2x user_tabs |
| Refresh 1 tab | Closes old, opens new session | Brief disconnect/reconnect (debounced) |
| Close 1 of 3 tabs | 1 session removed | user_tabs (2 remaining) |
| Close all tabs | All sessions removed | user_offline |
| Logout | All sessions closed (code 1000) | Immediate user_offline |

---

## Current Gaps & Considerations

### 1. ❌ No Device ID Passed from Frontend

**Issue**: Frontend doesn't pass `device_id` parameter

**Current Code**:
```typescript
const url = `${wsUrl}/ws?scope_id=${scopeId}&token=${token}`
// Missing: &device_id=<device_id>
```

**Impact**:
- Can't distinguish between multiple devices per user
- Can't show "Alice (Desktop)" vs "Alice (Mobile)"
- Device-level presence unavailable

**Solution**:
```typescript
const deviceId = getOrCreateDeviceId() // From localStorage
const url = `${wsUrl}/ws?scope_id=${scopeId}&token=${token}&device_id=${deviceId}&device_type=desktop`

function getOrCreateDeviceId(): string {
    let deviceId = localStorage.getItem('userengine_device_id')
    if (!deviceId) {
        deviceId = `device-${crypto.randomUUID()}`
        localStorage.setItem('userengine_device_id', deviceId)
    }
    return deviceId
}
```

### 2. ⚠️ Session Persistence Across Page Navigation

**Current Behavior**: Connection maintained as singleton
**Risk**: If page completely reloads, connection drops and recreates

**Monitor Component Issue**:
```typescript
onUnmounted(() => {
    // Clean up event listeners but DON'T disconnect
    // UserEngine connection should persist across page navigation
    // It will disconnect automatically on logout
})
```

**Good**: Doesn't disconnect on component unmount  
**Concern**: Relies on global singleton state

### 3. ⚠️ No Tab Visibility API Integration

**Missing Optimization**: Desktop apps typically pause activity on background tabs

**Potential Enhancement**:
```typescript
document.addEventListener('visibilitychange', () => {
    if (document.hidden) {
        // Tab is hidden - could reduce heartbeat frequency
        // Or mark as "inactive" but keep connection
    } else {
        // Tab is visible - restore normal heartbeat
    }
})
```

### 4. ✅ Heartbeat Properly Configured

**Current Implementation**:
```typescript
this.pingInterval = setInterval(() => {
    if (this.socket && this.socket.readyState === WebSocket.OPEN) {
        this.socket.send(JSON.stringify({ type: 'ping', ... }))
    }
}, 4000) // 4s ping interval
```

**Server Config**: 5s timeout (HEARTBEAT_INTERVAL)  
**Assessment**: ✅ Good (4s < 5s leaves margin for network latency)

### 5. ❌ No Explicit Tab Count Tracking in Frontend

**Issue**: Frontend tracks `onlineUsersCount` but not `tabCount` per user

**Current Stats**:
```typescript
interface UserEngineStats {
    onlineUsers: number      // ✅ Tracked
    activeChannels: number   // ✅ Tracked
    latency: number          // ✅ Tracked
    // ❌ Missing: userTabCounts: Map<string, number>
}
```

**Impact**: Can't show "Alice (3 tabs)" in UI

**Solution**:
```typescript
interface UserEngineStats {
    onlineUsers: number
    activeChannels: number
    latency: number
    userTabCounts: Map<string, number> // NEW
}

// Listen for tab events
userEngine.on('user_tabs', (event) => {
    userTabCounts.set(event.payload.user_id, event.payload.tab_count)
})
```

### 6. ⚠️ No Session Recovery on Reconnect

**Current Behavior**: If connection drops, creates **new session** on reconnect

**Potential Issue**: Rapid reconnects create new sessions each time
- Session count temporarily inflated
- Old sessions cleaned up by sweeper (30s default)

**Better Approach** (not implemented):
- Store `sessionID` in `sessionStorage` (per-tab)
- On reconnect, send same `sessionID` to server
- Server checks if session still valid (within TTL)
- Resume session instead of creating new one

---

## Redis Session Data Structure

### Keys Used

```redis
# Session metadata
session:{scope_id}:{session_id}
TTL: 60s
Type: String (JSON)

# User's active sessions
user_sessions:{scope_id}:{user_id}
Type: Set
Members: [session-1, session-2, ...]

# All online users in scope
online_users:{scope_id}
Type: Set
Members: [alice, bob, charlie]

# User version counter (for event ordering)
user_version:{scope_id}:{user_id}
Type: String (integer)

# Device breakdown (optional)
user_devices:{scope_id}:{user_id}
Type: Hash
Fields: {desktop: 2, mobile: 1}

# Debounce jobs
debounce:job:{scope_id}:{user_id}:{session_id}
TTL: 5s (debounce window)
Type: String (JSON)
```

### Example Data

**User "alice" with 2 desktop tabs + 1 mobile session**:

```redis
# Sessions
session:ws-1:sess-001 = {"user_id":"alice","device_type":"desktop",...}
session:ws-1:sess-002 = {"user_id":"alice","device_type":"desktop",...}
session:ws-1:sess-003 = {"user_id":"alice","device_type":"mobile",...}

# User sessions set
user_sessions:ws-1:alice = {sess-001, sess-002, sess-003}

# Online users
online_users:ws-1 = {alice, bob, charlie}

# Device breakdown
user_devices:ws-1:alice = {desktop: 2, mobile: 1}

# Version
user_version:ws-1:alice = 42
```

---

## Background Workers

### 1. Sweeper Worker

**Purpose**: Clean up expired sessions  
**Interval**: 30 seconds (default)  
**Logic**:
1. Scan all sessions in Redis
2. Check if TTL expired
3. If expired → Remove session
4. If user's last session → Publish `user_offline` event

**Location**: [`sweeper.go`](d:\DEV\UNDER_DEVELOPMENT\personal\BACKENDs\userengine\pkg\presence\sweeper.go)

### 2. Debouncer Worker

**Purpose**: Handle delayed disconnects  
**Interval**: Polls debounce job queue  
**Logic**:
1. Check for debounce jobs with expired TTL
2. If job still exists after delay → Execute disconnect
3. If session reconnected → Job deleted (disconnect cancelled)

**Location**: [`debouncer.go`](d:\DEV\UNDER_DEVELOPMENT\personal\BACKENDs\userengine\pkg\presence\debouncer.go)

---

## Recommendations

### Priority 1: Add Device ID Tracking

**Impact**: High  
**Effort**: Low  

**Changes**:
```typescript
// frontend/shared-api/src/userengine/UserEngineClient.ts

function getOrCreateDeviceId(): string {
    let deviceId = localStorage.getItem('ue_device_id')
    if (!deviceId) {
        deviceId = `device-${crypto.randomUUID()}`
        localStorage.setItem('ue_device_id', deviceId)
    }
    return deviceId
}

function detectDeviceType(): string {
    const ua = navigator.userAgent
    if (/mobile/i.test(ua)) return 'mobile'
    if (/tablet/i.test(ua)) return 'tablet'
    return 'desktop'
}

public async connect() {
    // ... existing code ...
    const deviceId = getOrCreateDeviceId()
    const deviceType = detectDeviceType()
    const url = `${wsUrl}/ws?scope_id=${scopeId}&token=${token}&device_id=${deviceId}&device_type=${deviceType}`
    // ...
}
```

### Priority 2: Track Tab Counts in Frontend

**Impact**: Medium  
**Effort**: Low  

**Changes**:
```typescript
interface UserEngineStats {
    onlineUsers: number
    activeChannels: number
    latency: number
    userTabCounts: Map<string, number> // NEW
}

// In handleMessage()
if (msg.type === 'user_tabs' || msg.type === 'user_online' || msg.type === 'user_offline') {
    this.stats.userTabCounts.set(msg.payload.user_id, msg.payload.tab_count || 0)
}
```

**UI Update** (ue_monitor.vue):
```vue
<template>
  <li v-for="user in onlineUsersList" :key="user.id" class="user-item">
    <div class="user-details">
      <span class="user-name">{{ user.username }}</span>
      <span class="user-sessions">{{ getUserTabCount(user.id) }} tabs</span>
    </div>
  </li>
</template>

<script setup>
const getUserTabCount = (userId: string) => {
  return userEngine.getStats().userTabCounts.get(userId) || 1
}
</script>
```

### Priority 3: Session Recovery on Reconnect

**Impact**: Low  
**Effort**: Medium  

Use `sessionStorage` (per-tab) to persist session ID across page reloads:

```typescript
public async connect() {
    // Try to recover existing session for this tab
    let sessionId = sessionStorage.getItem('ue_session_id')
    
    const url = sessionId 
        ? `${wsUrl}/ws?scope_id=${scopeId}&token=${token}&session_id=${sessionId}`
        : `${wsUrl}/ws?scope_id=${scopeId}&token=${token}`
    
    this.socket = new WebSocket(url)
    
    // On successful connect, store session ID
    this.socket.onopen = () => {
        // Server would need to send session_id in welcome message
        // sessionStorage.setItem('ue_session_id', receivedSessionId)
    }
}
```

**Backend Change Required**: Server must accept `session_id` param and validate/resume if valid

---

## Testing Scenarios

### Manual Testing Checklist

- [ ] Open 1 tab → Verify `user_online` event
- [ ] Open 2nd tab → Verify `user_tabs` event (tab_count: 2)
- [ ] Close 1 tab → Verify `user_tabs` event (tab_count: 1)
- [ ] Close last tab → Verify `user_offline` event
- [ ] Refresh tab → Verify debounce prevents flapping
- [ ] Logout → Verify immediate disconnect (no debounce)
- [ ] Network disconnect → Verify debounce + recovery
- [ ] Multiple devices → Verify separate session tracking
- [ ] Invisible mode → Verify user appears offline

### Automated Tests

**Location**: [`BACKENDs/userengine/test/`](d:\DEV\UNDER_DEVELOPMENT\personal\BACKENDs\userengine\test)

Existing tests:
- `presence_test.go` - Core presence logic
- `webhooks_test.go` - Event publishing

---

## Summary

### ✅ What Works Well

1. **Automatic session per tab** - No manual tracking needed
2. **Robust debounce logic** - Prevents flapping on reconnects
3. **Event versioning** - Handles out-of-order delivery
4. **Heartbeat mechanism** - Reliable keep-alive
5. **TTL-based expiration** - Automatic cleanup of stale sessions

### ⚠️ Areas for Improvement

1. **No device ID** - Can't distinguish devices
2. **No tab count UI** - Frontend doesn't display tab counts
3. **No session recovery** - Creates new session on every reconnect
4. **No visibility API** - Could optimize background tabs

### 🎯 Recommended Next Steps

1. **Implement device ID tracking** (1-2 hours)
2. **Add tab count to UI** (1 hour)
3. **Create session tracking guide** (documentation)
4. **Add monitoring dashboard** for session metrics
