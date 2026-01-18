# Django Integration Guide

Integrate UserEngine presence into your Django application.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Your Django App                          │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │   Views     │  │  Channels   │  │  Celery Tasks       │  │
│  │  (REST)     │  │ (WebSocket) │  │  (Background)       │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘  │
│         │                │                     │             │
│         ▼                ▼                     ▼             │
│  ┌─────────────────────────────────────────────────────────┐│
│  │              PresenceClient (HTTP + Redis)              ││
│  └─────────────────────────────────────────────────────────┘│
└─────────────────────┬───────────────────────┬───────────────┘
                      │                       │
            HTTP/WS   │                       │ Redis Pub/Sub
                      ▼                       ▼
         ┌────────────────────┐    ┌─────────────────────┐
         │  UserEngine        │    │       Redis         │
         │  (Gateway + API)   │◄───│                     │
         └────────────────────┘    └─────────────────────┘
```

---

## Setup

### 1. Install Dependencies

```bash
pip install requests redis pyjwt
```

### 2. Create Presence Client

Create `presence/client.py`:

```python
import json
import time
import hmac
import hashlib
from typing import Dict, List, Optional
from dataclasses import dataclass
from datetime import datetime, timedelta

import jwt
import redis
import requests


@dataclass
class PresenceConfig:
    """Configuration for UserEngine integration."""
    api_url: str = "http://localhost:8081"
    gateway_url: str = "ws://localhost:8080"
    redis_url: str = "redis://localhost:6379/0"
    jwt_secret: str = "dev-secret-change-in-production"
    jwt_algorithm: str = "HS256"
    token_expiry_hours: int = 24


class PresenceClient:
    """Client for UserEngine presence service."""
    
    def __init__(self, config: Optional[PresenceConfig] = None):
        self.config = config or PresenceConfig()
        self._redis: Optional[redis.Redis] = None
    
    @property
    def redis(self) -> redis.Redis:
        """Lazy Redis connection."""
        if self._redis is None:
            self._redis = redis.from_url(self.config.redis_url)
        return self._redis
    
    # ─────────────────────────────────────────────────────────
    # JWT Token Generation
    # ─────────────────────────────────────────────────────────
    
    def generate_token(self, user_id: str, scopes: List[str], 
                       expiry_hours: Optional[int] = None) -> str:
        """
        Generate JWT token for a user to connect to UserEngine.
        
        Args:
            user_id: Unique user identifier
            scopes: List of workspace/scope IDs user can access
            expiry_hours: Token validity period (default from config)
        
        Returns:
            JWT token string
        """
        expiry = expiry_hours or self.config.token_expiry_hours
        payload = {
            "user_id": user_id,
            "scopes": scopes,
            "exp": datetime.utcnow() + timedelta(hours=expiry),
            "iat": datetime.utcnow(),
        }
        return jwt.encode(payload, self.config.jwt_secret, 
                         algorithm=self.config.jwt_algorithm)
    
    # ─────────────────────────────────────────────────────────
    # REST API Methods
    # ─────────────────────────────────────────────────────────
    
    def lookup_users(self, scope_id: str, user_ids: List[str], 
                     token: str) -> Dict[str, str]:
        """
        Lookup presence status for multiple users.
        
        Args:
            scope_id: Workspace/scope identifier
            user_ids: List of user IDs to check
            token: JWT token for authentication
        
        Returns:
            Dict mapping user_id to status ("online" or "offline")
        """
        response = requests.post(
            f"{self.config.api_url}/presence/lookup",
            headers={
                "Authorization": f"Bearer {token}",
                "Content-Type": "application/json",
            },
            json={
                "scope_id": scope_id,
                "user_ids": user_ids,
            },
            timeout=5,
        )
        response.raise_for_status()
        return response.json().get("statuses", {})
    
    def get_online_users(self, scope_id: str, token: str) -> List[str]:
        """
        Get all online users in a scope.
        
        Args:
            scope_id: Workspace/scope identifier
            token: JWT token for authentication
        
        Returns:
            List of online user IDs
        """
        response = requests.get(
            f"{self.config.api_url}/presence/{scope_id}/online",
            headers={"Authorization": f"Bearer {token}"},
            timeout=5,
        )
        response.raise_for_status()
        return response.json().get("users", [])
    
    # ─────────────────────────────────────────────────────────
    # Direct Redis Methods (Server-Side)
    # ─────────────────────────────────────────────────────────
    
    def is_user_online(self, scope_id: str, user_id: str) -> bool:
        """
        Check if user is online (direct Redis, no auth needed).
        
        Use this for server-side checks where you don't need HTTP.
        """
        key = f"presence:scope:{scope_id}:online"
        return self.redis.sismember(key, user_id)
    
    def get_online_users_direct(self, scope_id: str) -> List[str]:
        """
        Get online users directly from Redis.
        
        Use this for server-side operations.
        """
        key = f"presence:scope:{scope_id}:online"
        members = self.redis.smembers(key)
        return [m.decode() if isinstance(m, bytes) else m for m in members]
    
    def get_user_session_count(self, scope_id: str, user_id: str) -> int:
        """Get number of active sessions for a user."""
        key = f"presence:scope:{scope_id}:user:{user_id}:sessions"
        return self.redis.scard(key)
    
    # ─────────────────────────────────────────────────────────
    # Redis Pub/Sub (Server-Side Events)
    # ─────────────────────────────────────────────────────────
    
    def subscribe_events(self, scope_id: str):
        """
        Subscribe to presence events for a scope.
        
        Yields:
            Dict with event data (type, user_id, version, etc.)
        
        Example:
            for event in client.subscribe_events("workspace-1"):
                if event["type"] == "user_online":
                    notify_team(event["user_id"])
        """
        pubsub = self.redis.pubsub()
        channel = f"presence:events:{scope_id}"
        pubsub.subscribe(channel)
        
        try:
            for message in pubsub.listen():
                if message["type"] == "message":
                    data = message["data"]
                    if isinstance(data, bytes):
                        data = data.decode()
                    yield json.loads(data)
        finally:
            pubsub.unsubscribe(channel)
            pubsub.close()


# Singleton instance
presence_client = PresenceClient()
```

### 3. Django Settings

Add to `settings.py`:

```python
# UserEngine Presence Configuration
PRESENCE_CONFIG = {
    "API_URL": os.environ.get("PRESENCE_API_URL", "http://localhost:8081"),
    "GATEWAY_URL": os.environ.get("PRESENCE_GATEWAY_URL", "ws://localhost:8080"),
    "REDIS_URL": os.environ.get("PRESENCE_REDIS_URL", "redis://localhost:6379/0"),
    "JWT_SECRET": os.environ.get("PRESENCE_JWT_SECRET", "your-secret-key"),
}
```

---

## Usage Examples

### Generate Token on Login

```python
# views.py
from django.contrib.auth.decorators import login_required
from django.http import JsonResponse
from presence.client import presence_client

@login_required
def get_presence_token(request):
    """Generate presence token for authenticated user."""
    user = request.user
    
    # Get user's workspace memberships
    workspaces = user.workspace_set.values_list('id', flat=True)
    scopes = [str(ws) for ws in workspaces]
    
    token = presence_client.generate_token(
        user_id=str(user.id),
        scopes=scopes,
    )
    
    return JsonResponse({
        "token": token,
        "gateway_url": presence_client.config.gateway_url,
    })
```

### Check User Presence in Views

```python
# views.py
from presence.client import presence_client

def workspace_members(request, workspace_id):
    """Show workspace members with online status."""
    workspace = get_object_or_404(Workspace, id=workspace_id)
    members = workspace.members.all()
    
    # Get online status for all members
    member_ids = [str(m.id) for m in members]
    
    # Option 1: Via API (if you need auth validation)
    token = presence_client.generate_token("system", [str(workspace_id)])
    statuses = presence_client.lookup_users(str(workspace_id), member_ids, token)
    
    # Option 2: Direct Redis (faster, server-side)
    # statuses = {
    #     uid: "online" if presence_client.is_user_online(str(workspace_id), uid) 
    #          else "offline"
    #     for uid in member_ids
    # }
    
    return render(request, "members.html", {
        "members": members,
        "statuses": statuses,
    })
```

### Real-Time Events with Celery

```python
# tasks.py
from celery import shared_task
from presence.client import presence_client

@shared_task
def listen_presence_events(scope_id: str):
    """
    Background task to listen for presence events.
    Run this as a long-running task or use Celery beat.
    """
    for event in presence_client.subscribe_events(scope_id):
        if event["type"] == "user_online":
            handle_user_online(scope_id, event["user_id"])
        elif event["type"] == "user_offline":
            handle_user_offline(scope_id, event["user_id"])

def handle_user_online(scope_id, user_id):
    """Handle user coming online."""
    # Send notification, update cache, etc.
    from channels.layers import get_channel_layer
    channel_layer = get_channel_layer()
    
    # Broadcast to workspace channel
    async_to_sync(channel_layer.group_send)(
        f"workspace_{scope_id}",
        {
            "type": "presence.update",
            "user_id": user_id,
            "status": "online",
        }
    )
```

### Django Channels Consumer

```python
# consumers.py
import json
from channels.generic.websocket import AsyncJsonWebsocketConsumer

class WorkspaceConsumer(AsyncJsonWebsocketConsumer):
    """WebSocket consumer that includes presence updates."""
    
    async def connect(self):
        self.workspace_id = self.scope["url_route"]["kwargs"]["workspace_id"]
        self.group_name = f"workspace_{self.workspace_id}"
        
        await self.channel_layer.group_add(self.group_name, self.channel_name)
        await self.accept()
    
    async def disconnect(self, code):
        await self.channel_layer.group_discard(self.group_name, self.channel_name)
    
    async def presence_update(self, event):
        """Handle presence update from Celery task."""
        await self.send_json({
            "type": "presence",
            "user_id": event["user_id"],
            "status": event["status"],
        })
```

---

## Frontend Integration

### JavaScript WebSocket Client

```javascript
// presence.js
class PresenceManager {
    constructor(gatewayUrl, token, scopeId) {
        this.gatewayUrl = gatewayUrl;
        this.token = token;
        this.scopeId = scopeId;
        this.ws = null;
        this.userVersions = new Map();
        this.listeners = [];
    }
    
    connect() {
        const url = `${this.gatewayUrl}/ws?scope_id=${this.scopeId}&token=${this.token}`;
        this.ws = new WebSocket(url);
        
        this.ws.onopen = () => {
            console.log('Presence connected');
            this.startHeartbeat();
        };
        
        this.ws.onmessage = (event) => {
            const data = JSON.parse(event.data);
            this.handleEvent(data);
        };
        
        this.ws.onclose = (event) => {
            console.log('Presence disconnected', event.code);
            if (event.code === 4001) {
                // Reconnect required - get new token
                this.onReconnectRequired?.();
            } else {
                // Auto-reconnect after delay
                setTimeout(() => this.connect(), 3000);
            }
        };
    }
    
    handleEvent(event) {
        // Deduplicate using version
        const lastVersion = this.userVersions.get(event.user_id) || 0;
        if (event.version <= lastVersion) {
            return; // Stale event
        }
        this.userVersions.set(event.user_id, event.version);
        
        // Notify listeners
        this.listeners.forEach(fn => fn(event));
    }
    
    onPresenceChange(callback) {
        this.listeners.push(callback);
    }
    
    startHeartbeat() {
        setInterval(() => {
            if (this.ws?.readyState === WebSocket.OPEN) {
                this.ws.send(JSON.stringify({ type: 'heartbeat' }));
            }
        }, 20000);
    }
    
    disconnect() {
        this.ws?.close();
    }
}

// Usage
const presence = new PresenceManager(
    'ws://localhost:8080',
    presenceToken, // From Django view
    workspaceId
);

presence.onPresenceChange((event) => {
    const userEl = document.querySelector(`[data-user-id="${event.user_id}"]`);
    if (userEl) {
        userEl.classList.toggle('online', event.type === 'user_online');
    }
});

presence.connect();
```

### Django Template

```html
<!-- templates/workspace.html -->
{% load static %}

<script>
    const presenceConfig = {
        token: "{{ presence_token }}",
        gatewayUrl: "{{ presence_gateway_url }}",
        scopeId: "{{ workspace.id }}",
    };
</script>
<script src="{% static 'js/presence.js' %}"></script>

<ul class="member-list">
    {% for member in members %}
    <li data-user-id="{{ member.id }}" 
        class="{% if statuses|get:member.id == 'online' %}online{% endif %}">
        {{ member.username }}
        <span class="status-dot"></span>
    </li>
    {% endfor %}
</ul>

<style>
    .status-dot {
        width: 8px;
        height: 8px;
        border-radius: 50%;
        background: #ccc;
        display: inline-block;
    }
    .online .status-dot {
        background: #22c55e;
    }
</style>

<script>
    const presence = new PresenceManager(
        presenceConfig.gatewayUrl,
        presenceConfig.token,
        presenceConfig.scopeId
    );
    presence.onPresenceChange(event => {
        const el = document.querySelector(`[data-user-id="${event.user_id}"]`);
        if (el) {
            el.classList.toggle('online', event.type === 'user_online');
        }
    });
    presence.connect();
</script>
```

---

## Testing

```python
# tests/test_presence.py
from django.test import TestCase
from presence.client import PresenceClient, PresenceConfig
from unittest.mock import patch, MagicMock

class PresenceClientTest(TestCase):
    def setUp(self):
        self.client = PresenceClient(PresenceConfig(
            jwt_secret="test-secret"
        ))
    
    def test_generate_token(self):
        token = self.client.generate_token("user-1", ["workspace-1"])
        self.assertIsInstance(token, str)
        self.assertTrue(len(token) > 50)
    
    @patch('requests.post')
    def test_lookup_users(self, mock_post):
        mock_post.return_value.json.return_value = {
            "statuses": {"user-1": "online", "user-2": "offline"}
        }
        mock_post.return_value.raise_for_status = MagicMock()
        
        token = self.client.generate_token("system", ["ws-1"])
        result = self.client.lookup_users("ws-1", ["user-1", "user-2"], token)
        
        self.assertEqual(result["user-1"], "online")
        self.assertEqual(result["user-2"], "offline")
```

---

## Quick Reference

```python
from presence.client import presence_client

# Generate token for user
token = presence_client.generate_token(user_id, workspace_ids)

# Lookup via API
statuses = presence_client.lookup_users(scope_id, user_ids, token)

# Direct Redis check (server-side)
is_online = presence_client.is_user_online(scope_id, user_id)
online_users = presence_client.get_online_users_direct(scope_id)

# Subscribe to events (blocking)
for event in presence_client.subscribe_events(scope_id):
    print(f"{event['user_id']} is now {event['type']}")
```

