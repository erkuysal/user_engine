# UserEngine Django Implementation Guide

The **UserEngine** integration allows your Django application to handle real-time user presence, tab tracking, and online status efficiently using a dedicated Go-based microservice.

This guide provides a comprehensive, step-by-step implementation plan for integrating UserEngine into a Django backend.

---

## 1. System Architecture

The integration involves three distinct components interacting:

1.  **Django Backend (Source of Truth)**
    *   Authenticates users.
    *   Generates **JWT Tokens** for the frontend to connect to UserEngine.
    *   Proxies presence queries (e.g., "Is User X online?") via HTTP to UserEngine.
    *   Handles "Logout" by explicitly telling UserEngine to disconnect a user.

2.  **UserEngine Service (Presence Hub)**
    *   Maintains WebSocket connections with Frontends.
    *   Tracks "Heartbeats" to determine online status.
    *   Stores state in Redis.
    *   Exposes an HTTP API for the Backend to query/control state.

3.  **Frontend (Client)**
    *   Logs in via Django to get a Token.
    *   Connects to UserEngine WebSocket with that Token.
    *   Sends periodic heartbeats (handled automatically by the SDK/WebSocket logic).
    *   Can query presence via Django API or UserEngine directly.

---

## 2. Prerequisites

*   **Django Project** (v3.2+)
*   **UserEngine Service** running (default ports: API `:8081`, Gateway `:8080`)
*   **Redis** (used by UserEngine)

---

## 3. Django Configuration (`settings.py`)

Add the `USERENGINE` configuration dictionary to `settings.py`. Use `os.environ` to allow overriding these values for different environments (e.g., Local vs Docker vs WSL).

```python
import os

USERENGINE = {
    # API_URL: The internal URL for Django to talk to UserEngine HTTP API.
    # CRITICAL: In Docker/WSL, 'localhost' refers to the container, not the host.
    # Use the Host IP (e.g., 172.x.x.x) or service name.
    'API_URL': os.environ.get('USERENGINE_API_URL', 'http://localhost:8081'),
    
    # WS_URL: The public URL for the Frontend to connect via WebSocket.
    # This must be reachable by the user's browser.
    'WS_URL': os.environ.get('USERENGINE_WS_URL', 'ws://localhost:8080'),
    
    # SCOPE_ID: A unique namespace for your app's users.
    'SCOPE_ID': os.environ.get('USERENGINE_SCOPE', 'default_scope'),
    
    # TOKEN: The Service Token allowing Django to make admin calls to UserEngine.
    # This token must be generated using the UserEngine's JWT_SECRET.
    'TOKEN': os.environ.get('USERENGINE_TOKEN', None),
    
    # JWT_SECRET: Shared secret for signing frontend tokens.
    # MUST match the JWT_SECRET used by UserEngine.
    'JWT_SECRET': os.environ.get('USERENGINE_PRESENCE_JWT_SECRET', 'dev-secret-change-in-production'),
}

# Ensure your ALLOWED_HOSTS includes the host IP if running in WSL/Docker
# ALLOWED_HOSTS = ['localhost', '127.0.0.1', '*'] 
```

---

## 4. Backend Implementation

### A. Token Generation (`core/utils.py`)
You must generate a JWT signed with `HS256` that UserEngine validates.

```python
import hmac
import hashlib
import base64
import json
import time
from django.conf import settings

def generate_user_token(user_id, secret):
    """
    Generates a JWT token for UserEngine WebSocket authentication.
    """
    header = {'alg': 'HS256', 'typ': 'JWT'}
    payload = {
        'user_id': str(user_id),
        'scopes': [settings.USERENGINE['SCOPE_ID']],
        'iat': int(time.time()),
        # Token valid for 5 minutes (enough to establish WS connection)
        'exp': int(time.time()) + 300 
    }
    
    def base64url(data):
        return base64.urlsafe_b64encode(json.dumps(data).encode('utf-8')).decode('utf-8').rstrip('=')

    h = base64url(header)
    p = base64url(payload)
    
    # Create Signature
    signature = hmac.new(
        secret.encode('utf-8'),
        f"{h}.{p}".encode('utf-8'),
        hashlib.sha256
    ).digest()
    
    sig = base64.urlsafe_b64encode(signature).decode('utf-8').rstrip('=')
    return f"{h}.{p}.{sig}"
```

### B. API Adapter (`core/adapters.py`)
Create an adapter to handle HTTP communication with UserEngine.

```python
import json
import urllib.request
from django.conf import settings

class UserEngineAdapter:
    def __init__(self):
        self.base_url = settings.USERENGINE['API_URL'].rstrip('/')
        self.token = settings.USERENGINE['TOKEN']

    def _request(self, method, path, body=None):
        url = f"{self.base_url}{path}"
        headers = {'Content-Type': 'application/json'}
        if self.token:
            headers['Authorization'] = f"Bearer {self.token}"

        data = json.dumps(body).encode('utf-8') if body else None
        req = urllib.request.Request(url, data=data, headers=headers, method=method)

        with urllib.request.urlopen(req) as response:
            return json.loads(response.read().decode('utf-8'))

    def disconnect_user(self, scope_id, user_id):
        return self._request('POST', '/presence/disconnect_user', {
            'scope_id': scope_id,
            'user_id': str(user_id)
        })

    def get_online_users(self, scope_id):
        # ... Implementation for GET /presence/online
        pass
```

### C. Views (`core/views.py`)
Expose these API endpoints for your frontend.

```python
from django.http import JsonResponse
from django.views.decorators.http import require_http_methods
from .utils import generate_user_token
from .adapters import UserEngineAdapter

@require_http_methods(["POST"])
def get_userengine_token(request):
    """
    Frontend calls this to get a token to connect to UserEngine.
    """
    # ... Authentication check here ...
    user_id = request.user.id # Or from body
    
    token = generate_user_token(user_id, settings.USERENGINE['JWT_SECRET'])
    
    return JsonResponse({
        'token': token,
        'wsUrl': settings.USERENGINE['WS_URL'],
        'scopeId': settings.USERENGINE['SCOPE_ID']
    })

@require_http_methods(["POST"])
def logout_user(request):
    """
    Cleans up UserEngine session when user logs out of Django.
    """
    user_id = request.POST.get('userId')
    adapter = UserEngineAdapter()
    adapter.disconnect_user(settings.USERENGINE['SCOPE_ID'], user_id)
    return JsonResponse({'success': True})
```

---

## 5. Development Environment Setup (WSL / Docker)

A common issue is network connectivity between a Django app running in a virtualized environment (WSL, Docker) and the UserEngine running on the Windows host.

### The Problem
If Django is in WSL, `localhost:8081` points to the WSL VM, not your Windows machine where UserEngine is running.

### The Solution: Use Host IP

1.  **Get Host IP** (Run inside WSL):
    ```bash
    ip route show default | awk '{print $3}'
    # Output example: 172.25.160.1
    ```

2.  **Generate Service Token**:
    You need a long-lived JWT token for your Django backend to authenticate with UserEngine as an admin/service.
    ```python
    # Run this python one-liner to generate a token (uses dev secret)
    python -c "import hmac, hashlib, base64, json, time; secret = 'dev-secret-change-in-production'; header = {'alg': 'HS256', 'typ': 'JWT'}; payload = {'user_id': 'django-service', 'scopes': ['*'], 'iat': int(time.time()), 'exp': int(time.time()) + 315360000}; base64url = lambda x: base64.urlsafe_b64encode(json.dumps(x, separators=(',', ':')).encode('utf-8')).decode('utf-8').rstrip('='); h = base64url(header); p = base64url(payload); sig = base64.urlsafe_b64encode(hmac.new(secret.encode('utf-8'), f'{h}.{p}'.encode('utf-8'), hashlib.sha256).digest()).decode('utf-8').rstrip('='); print(f'{h}.{p}.{sig}')"
    ```

3.  **Start Django with Env Vars**:
    ```bash
    export USERENGINE_API_URL="http://172.25.160.1:8081"
    export USERENGINE_WS_URL="ws://172.25.160.1:8080"
    export USERENGINE_TOKEN="<PASTE_GENERATED_TOKEN_HERE>"
    
    python manage.py runserver 0.0.0.0:8000
    ```

---

## 6. Troubleshooting

### 401 Unauthorized (`UserEngine: No valid auth token`)
*   **Cause**: The `USERENGINE_TOKEN` in `settings.py` is missing or invalid.
*   **Fix**: Generate a new service token (Step 5.2) and export it as `USERENGINE_TOKEN`.

### Connection Refused (`[Errno 111]`)
*   **Cause**: Django cannot reach UserEngine. Usually because `localhost` is used in WSL.
*   **Fix**: Update `USERENGINE_API_URL` to use the Host IP.

### Zombie Tabs / Slow Disconnect
*   **Cause**: `SESSION_TTL` in UserEngine config is too high (default 60s).
*   **Fix**: For development, edit `UserEngine/.env` and set:
    ```ini
    SESSION_TTL=12s
    HEARTBEAT_INTERVAL=5s
    ```
    Active tabs will heartbeat every 5s. Dead tabs will be cleaned up in ~12s.
