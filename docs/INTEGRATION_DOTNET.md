# .NET Integration Guide

Integrate UserEngine presence into your ASP.NET Core application.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                   Your .NET Application                      │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │ Controllers │  │  SignalR    │  │  Background         │  │
│  │   (REST)    │  │   Hubs      │  │  Services           │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘  │
│         │                │                     │             │
│         ▼                ▼                     ▼             │
│  ┌─────────────────────────────────────────────────────────┐│
│  │           IPresenceClient (HTTP + Redis)                ││
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

### 1. Install NuGet Packages

```bash
dotnet add package StackExchange.Redis
dotnet add package System.IdentityModel.Tokens.Jwt
dotnet add package Microsoft.AspNetCore.SignalR
```

### 2. Create Presence Client

Create `Services/PresenceClient.cs`:

```csharp
using System.IdentityModel.Tokens.Jwt;
using System.Net.Http.Json;
using System.Security.Claims;
using System.Text;
using System.Text.Json;
using Microsoft.IdentityModel.Tokens;
using StackExchange.Redis;

namespace YourApp.Services;

public class PresenceConfig
{
    public string ApiUrl { get; set; } = "http://localhost:8081";
    public string GatewayUrl { get; set; } = "ws://localhost:8080";
    public string RedisConnection { get; set; } = "localhost:6379";
    public string JwtSecret { get; set; } = "dev-secret-change-in-production";
    public int TokenExpiryHours { get; set; } = 24;
}

public record PresenceEvent(
    string EventId,
    string ScopeId,
    string Type,
    string UserId,
    long Version,
    DateTime OccurredAt,
    string Source
);

public record LookupResponse(Dictionary<string, string> Statuses);

public interface IPresenceClient
{
    string GenerateToken(string userId, IEnumerable<string> scopes);
    Task<Dictionary<string, string>> LookupUsersAsync(string scopeId, IEnumerable<string> userIds, string token);
    Task<List<string>> GetOnlineUsersAsync(string scopeId, string token);
    bool IsUserOnline(string scopeId, string userId);
    List<string> GetOnlineUsersDirect(string scopeId);
    IAsyncEnumerable<PresenceEvent> SubscribeEventsAsync(string scopeId, CancellationToken ct = default);
}

public class PresenceClient : IPresenceClient, IDisposable
{
    private readonly PresenceConfig _config;
    private readonly HttpClient _httpClient;
    private readonly Lazy<ConnectionMultiplexer> _redis;
    private readonly JwtSecurityTokenHandler _tokenHandler;
    private readonly SigningCredentials _signingCredentials;

    public PresenceClient(PresenceConfig config)
    {
        _config = config;
        _httpClient = new HttpClient { BaseAddress = new Uri(config.ApiUrl) };
        _redis = new Lazy<ConnectionMultiplexer>(() => 
            ConnectionMultiplexer.Connect(config.RedisConnection));
        _tokenHandler = new JwtSecurityTokenHandler();
        
        var key = new SymmetricSecurityKey(Encoding.UTF8.GetBytes(config.JwtSecret));
        _signingCredentials = new SigningCredentials(key, SecurityAlgorithms.HmacSha256);
    }

    private IDatabase Redis => _redis.Value.GetDatabase();

    // ─────────────────────────────────────────────────────────
    // JWT Token Generation
    // ─────────────────────────────────────────────────────────

    public string GenerateToken(string userId, IEnumerable<string> scopes)
    {
        var claims = new List<Claim>
        {
            new("user_id", userId),
        };
        
        foreach (var scope in scopes)
        {
            claims.Add(new Claim("scopes", scope));
        }

        var token = new JwtSecurityToken(
            expires: DateTime.UtcNow.AddHours(_config.TokenExpiryHours),
            claims: claims,
            signingCredentials: _signingCredentials
        );

        return _tokenHandler.WriteToken(token);
    }

    // ─────────────────────────────────────────────────────────
    // REST API Methods
    // ─────────────────────────────────────────────────────────

    public async Task<Dictionary<string, string>> LookupUsersAsync(
        string scopeId, 
        IEnumerable<string> userIds, 
        string token)
    {
        var request = new HttpRequestMessage(HttpMethod.Post, "/presence/lookup")
        {
            Content = JsonContent.Create(new { scope_id = scopeId, user_ids = userIds }),
            Headers = { { "Authorization", $"Bearer {token}" } }
        };

        var response = await _httpClient.SendAsync(request);
        response.EnsureSuccessStatusCode();

        var result = await response.Content.ReadFromJsonAsync<LookupResponse>();
        return result?.Statuses ?? new Dictionary<string, string>();
    }

    public async Task<List<string>> GetOnlineUsersAsync(string scopeId, string token)
    {
        var request = new HttpRequestMessage(HttpMethod.Get, $"/presence/{scopeId}/online")
        {
            Headers = { { "Authorization", $"Bearer {token}" } }
        };

        var response = await _httpClient.SendAsync(request);
        response.EnsureSuccessStatusCode();

        var result = await response.Content.ReadFromJsonAsync<OnlineResponse>();
        return result?.Users ?? new List<string>();
    }

    private record OnlineResponse(List<string> Users);

    // ─────────────────────────────────────────────────────────
    // Direct Redis Methods (Server-Side)
    // ─────────────────────────────────────────────────────────

    public bool IsUserOnline(string scopeId, string userId)
    {
        var key = $"presence:scope:{scopeId}:online";
        return Redis.SetContains(key, userId);
    }

    public List<string> GetOnlineUsersDirect(string scopeId)
    {
        var key = $"presence:scope:{scopeId}:online";
        return Redis.SetMembers(key)
            .Select(m => m.ToString())
            .ToList();
    }

    public int GetUserSessionCount(string scopeId, string userId)
    {
        var key = $"presence:scope:{scopeId}:user:{userId}:sessions";
        return (int)Redis.SetLength(key);
    }

    // ─────────────────────────────────────────────────────────
    // Redis Pub/Sub (Server-Side Events)
    // ─────────────────────────────────────────────────────────

    public async IAsyncEnumerable<PresenceEvent> SubscribeEventsAsync(
        string scopeId,
        [System.Runtime.CompilerServices.EnumeratorCancellation] CancellationToken ct = default)
    {
        var subscriber = _redis.Value.GetSubscriber();
        var channel = $"presence:events:{scopeId}";
        
        var queue = System.Threading.Channels.Channel.CreateUnbounded<PresenceEvent>();
        
        await subscriber.SubscribeAsync(channel, (_, message) =>
        {
            if (message.HasValue)
            {
                var evt = JsonSerializer.Deserialize<PresenceEvent>(message!, 
                    new JsonSerializerOptions { PropertyNameCaseInsensitive = true });
                if (evt != null)
                {
                    queue.Writer.TryWrite(evt);
                }
            }
        });

        try
        {
            await foreach (var evt in queue.Reader.ReadAllAsync(ct))
            {
                yield return evt;
            }
        }
        finally
        {
            await subscriber.UnsubscribeAsync(channel);
        }
    }

    public void Dispose()
    {
        _httpClient.Dispose();
        if (_redis.IsValueCreated)
        {
            _redis.Value.Dispose();
        }
    }
}
```

### 3. Register Services

In `Program.cs`:

```csharp
var builder = WebApplication.CreateBuilder(args);

// Add presence configuration
builder.Services.AddSingleton(new PresenceConfig
{
    ApiUrl = builder.Configuration["Presence:ApiUrl"] ?? "http://localhost:8081",
    GatewayUrl = builder.Configuration["Presence:GatewayUrl"] ?? "ws://localhost:8080",
    RedisConnection = builder.Configuration["Presence:RedisConnection"] ?? "localhost:6379",
    JwtSecret = builder.Configuration["Presence:JwtSecret"] ?? throw new Exception("JWT secret required"),
});

builder.Services.AddSingleton<IPresenceClient, PresenceClient>();
builder.Services.AddSignalR();
builder.Services.AddHostedService<PresenceEventListener>();

var app = builder.Build();
```

### 4. Configuration

In `appsettings.json`:

```json
{
  "Presence": {
    "ApiUrl": "http://localhost:8081",
    "GatewayUrl": "ws://localhost:8080",
    "RedisConnection": "localhost:6379",
    "JwtSecret": "your-secret-key-min-32-chars-long"
  }
}
```

---

## Usage Examples

### Generate Token on Login

```csharp
[ApiController]
[Route("api/[controller]")]
public class AuthController : ControllerBase
{
    private readonly IPresenceClient _presence;
    private readonly PresenceConfig _config;

    public AuthController(IPresenceClient presence, PresenceConfig config)
    {
        _presence = presence;
        _config = config;
    }

    [HttpPost("login")]
    public async Task<IActionResult> Login([FromBody] LoginRequest request)
    {
        // Your authentication logic...
        var user = await AuthenticateUser(request);
        if (user == null) return Unauthorized();

        // Get user's workspace memberships
        var workspaces = await GetUserWorkspaces(user.Id);
        var scopes = workspaces.Select(w => w.Id.ToString()).ToList();

        // Generate presence token
        var presenceToken = _presence.GenerateToken(user.Id.ToString(), scopes);

        return Ok(new
        {
            authToken = GenerateAuthToken(user),
            presenceToken = presenceToken,
            presenceGatewayUrl = _config.GatewayUrl,
        });
    }
}
```

### Check Presence in Controllers

```csharp
[ApiController]
[Route("api/workspaces/{workspaceId}")]
public class WorkspaceController : ControllerBase
{
    private readonly IPresenceClient _presence;

    public WorkspaceController(IPresenceClient presence)
    {
        _presence = presence;
    }

    [HttpGet("members")]
    public async Task<IActionResult> GetMembers(string workspaceId)
    {
        var members = await _memberService.GetMembersAsync(workspaceId);
        var memberIds = members.Select(m => m.UserId).ToList();

        // Option 1: Via API
        var token = _presence.GenerateToken("system", new[] { workspaceId });
        var statuses = await _presence.LookupUsersAsync(workspaceId, memberIds, token);

        // Option 2: Direct Redis (faster)
        // var statuses = memberIds.ToDictionary(
        //     id => id,
        //     id => _presence.IsUserOnline(workspaceId, id) ? "online" : "offline"
        // );

        return Ok(members.Select(m => new
        {
            m.UserId,
            m.DisplayName,
            Status = statuses.GetValueOrDefault(m.UserId, "offline")
        }));
    }

    [HttpGet("online")]
    public IActionResult GetOnlineUsers(string workspaceId)
    {
        // Direct Redis - no HTTP overhead
        var onlineUsers = _presence.GetOnlineUsersDirect(workspaceId);
        return Ok(new { users = onlineUsers, count = onlineUsers.Count });
    }
}
```

### Background Service for Events

```csharp
public class PresenceEventListener : BackgroundService
{
    private readonly IPresenceClient _presence;
    private readonly IHubContext<WorkspaceHub> _hubContext;
    private readonly ILogger<PresenceEventListener> _logger;

    public PresenceEventListener(
        IPresenceClient presence,
        IHubContext<WorkspaceHub> hubContext,
        ILogger<PresenceEventListener> logger)
    {
        _presence = presence;
        _hubContext = hubContext;
        _logger = logger;
    }

    protected override async Task ExecuteAsync(CancellationToken stoppingToken)
    {
        // Subscribe to events for all active workspaces
        // In production, you'd manage this dynamically
        var workspaces = new[] { "workspace-1", "workspace-2" };

        var tasks = workspaces.Select(ws => ListenToWorkspace(ws, stoppingToken));
        await Task.WhenAll(tasks);
    }

    private async Task ListenToWorkspace(string workspaceId, CancellationToken ct)
    {
        try
        {
            await foreach (var evt in _presence.SubscribeEventsAsync(workspaceId, ct))
            {
                _logger.LogInformation(
                    "Presence event: {Type} for {UserId} in {Scope}", 
                    evt.Type, evt.UserId, evt.ScopeId);

                // Broadcast to SignalR clients
                await _hubContext.Clients
                    .Group($"workspace:{workspaceId}")
                    .SendAsync("PresenceUpdate", new
                    {
                        userId = evt.UserId,
                        status = evt.Type == "user_online" ? "online" : "offline",
                        version = evt.Version
                    }, ct);
            }
        }
        catch (OperationCanceledException)
        {
            // Normal shutdown
        }
        catch (Exception ex)
        {
            _logger.LogError(ex, "Error listening to presence events for {Workspace}", workspaceId);
        }
    }
}
```

### SignalR Hub

```csharp
public class WorkspaceHub : Hub
{
    private readonly IPresenceClient _presence;

    public WorkspaceHub(IPresenceClient presence)
    {
        _presence = presence;
    }

    public async Task JoinWorkspace(string workspaceId)
    {
        await Groups.AddToGroupAsync(Context.ConnectionId, $"workspace:{workspaceId}");
        
        // Send current online users
        var onlineUsers = _presence.GetOnlineUsersDirect(workspaceId);
        await Clients.Caller.SendAsync("InitialPresence", onlineUsers);
    }

    public async Task LeaveWorkspace(string workspaceId)
    {
        await Groups.RemoveFromGroupAsync(Context.ConnectionId, $"workspace:{workspaceId}");
    }
}
```

---

## Frontend Integration

### Blazor Component

```razor
@* Components/PresenceIndicator.razor *@
@inject IPresenceClient Presence

<span class="presence-dot @(IsOnline ? "online" : "offline")"></span>

@code {
    [Parameter] public string WorkspaceId { get; set; } = "";
    [Parameter] public string UserId { get; set; } = "";
    
    private bool IsOnline { get; set; }

    protected override void OnInitialized()
    {
        IsOnline = Presence.IsUserOnline(WorkspaceId, UserId);
    }
}

<style>
    .presence-dot {
        width: 10px;
        height: 10px;
        border-radius: 50%;
        display: inline-block;
    }
    .presence-dot.online { background: #22c55e; }
    .presence-dot.offline { background: #9ca3af; }
</style>
```

### JavaScript Client

```javascript
// wwwroot/js/presence.js
class PresenceManager {
    constructor(gatewayUrl, token, scopeId) {
        this.gatewayUrl = gatewayUrl;
        this.token = token;
        this.scopeId = scopeId;
        this.ws = null;
        this.userVersions = new Map();
        this.onUpdate = null;
    }
    
    connect() {
        const url = `${this.gatewayUrl}/ws?scope_id=${this.scopeId}&token=${this.token}`;
        this.ws = new WebSocket(url);
        
        this.ws.onopen = () => this.startHeartbeat();
        
        this.ws.onmessage = (e) => {
            const event = JSON.parse(e.data);
            
            // Version-based deduplication
            const lastVer = this.userVersions.get(event.user_id) || 0;
            if (event.version <= lastVer) return;
            this.userVersions.set(event.user_id, event.version);
            
            this.onUpdate?.(event);
        };
        
        this.ws.onclose = (e) => {
            if (e.code === 4001) {
                // Token expired, need new token
                this.onReconnectRequired?.();
            } else {
                setTimeout(() => this.connect(), 3000);
            }
        };
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

// Usage with Blazor
window.initPresence = (gatewayUrl, token, scopeId, dotNetRef) => {
    const presence = new PresenceManager(gatewayUrl, token, scopeId);
    
    presence.onUpdate = (event) => {
        dotNetRef.invokeMethodAsync('OnPresenceUpdate', event.user_id, event.type);
    };
    
    presence.onReconnectRequired = () => {
        dotNetRef.invokeMethodAsync('OnReconnectRequired');
    };
    
    presence.connect();
    return presence;
};
```

### Razor Page with Real-Time Updates

```razor
@* Pages/Workspace.razor *@
@page "/workspace/{WorkspaceId}"
@inject IPresenceClient Presence
@inject IJSRuntime JS
@implements IAsyncDisposable

<h1>Workspace Members</h1>

<ul>
    @foreach (var member in Members)
    {
        <li class="@(OnlineUsers.Contains(member.Id) ? "online" : "")">
            @member.Name
            <span class="status">@(OnlineUsers.Contains(member.Id) ? "🟢" : "⚫")</span>
        </li>
    }
</ul>

@code {
    [Parameter] public string WorkspaceId { get; set; } = "";
    
    private List<Member> Members { get; set; } = new();
    private HashSet<string> OnlineUsers { get; set; } = new();
    private IJSObjectReference? _presenceJs;
    private DotNetObjectReference<Workspace>? _dotNetRef;

    protected override async Task OnInitializedAsync()
    {
        Members = await LoadMembers(WorkspaceId);
        OnlineUsers = Presence.GetOnlineUsersDirect(WorkspaceId).ToHashSet();
    }

    protected override async Task OnAfterRenderAsync(bool firstRender)
    {
        if (firstRender)
        {
            _dotNetRef = DotNetObjectReference.Create(this);
            var token = Presence.GenerateToken(CurrentUserId, new[] { WorkspaceId });
            
            _presenceJs = await JS.InvokeAsync<IJSObjectReference>(
                "initPresence", 
                "ws://localhost:8080", 
                token, 
                WorkspaceId, 
                _dotNetRef);
        }
    }

    [JSInvokable]
    public void OnPresenceUpdate(string userId, string eventType)
    {
        if (eventType == "user_online")
            OnlineUsers.Add(userId);
        else
            OnlineUsers.Remove(userId);
        
        StateHasChanged();
    }

    [JSInvokable]
    public async Task OnReconnectRequired()
    {
        // Refresh token and reconnect
        NavigationManager.NavigateTo(NavigationManager.Uri, forceLoad: true);
    }

    public async ValueTask DisposeAsync()
    {
        if (_presenceJs != null)
            await _presenceJs.InvokeVoidAsync("disconnect");
        _dotNetRef?.Dispose();
    }
}
```

---

## Testing

```csharp
// Tests/PresenceClientTests.cs
using Moq;
using Xunit;

public class PresenceClientTests
{
    private readonly PresenceClient _client;

    public PresenceClientTests()
    {
        _client = new PresenceClient(new PresenceConfig
        {
            JwtSecret = "test-secret-key-min-32-characters-long"
        });
    }

    [Fact]
    public void GenerateToken_ReturnsValidJwt()
    {
        var token = _client.GenerateToken("user-1", new[] { "workspace-1" });
        
        Assert.NotNull(token);
        Assert.Contains(".", token); // JWT format
    }

    [Fact]
    public void GenerateToken_IncludesCorrectClaims()
    {
        var token = _client.GenerateToken("user-123", new[] { "ws-1", "ws-2" });
        
        var handler = new JwtSecurityTokenHandler();
        var jwt = handler.ReadJwtToken(token);
        
        Assert.Equal("user-123", jwt.Claims.First(c => c.Type == "user_id").Value);
        Assert.Equal(2, jwt.Claims.Count(c => c.Type == "scopes"));
    }
}
```

---

## Quick Reference

```csharp
// Inject the client
public class MyService
{
    private readonly IPresenceClient _presence;
    
    public MyService(IPresenceClient presence) => _presence = presence;
    
    public void Example()
    {
        // Generate token
        var token = _presence.GenerateToken(userId, workspaceIds);
        
        // Lookup via API
        var statuses = await _presence.LookupUsersAsync(scopeId, userIds, token);
        
        // Direct Redis (server-side)
        var isOnline = _presence.IsUserOnline(scopeId, userId);
        var onlineUsers = _presence.GetOnlineUsersDirect(scopeId);
        
        // Subscribe to events
        await foreach (var evt in _presence.SubscribeEventsAsync(scopeId))
        {
            Console.WriteLine($"{evt.UserId}: {evt.Type}");
        }
    }
}
```

