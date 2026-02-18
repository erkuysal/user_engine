package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/userengine/presence/pkg/auth"
	"github.com/userengine/presence/pkg/config"
	"github.com/userengine/presence/pkg/events"
	"github.com/userengine/presence/pkg/metrics"
	"github.com/userengine/presence/pkg/presence"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// WebSocket close codes
const (
	CloseCodeSessionExpired = 4001
)

// Hub manages WebSocket connections and event fanout.
type Hub struct {
	svc       *presence.Service
	EventBus  events.EventBus
	cfg       *config.Config
	validator *auth.Validator
	debouncer *presence.Debouncer
	upgrader  websocket.Upgrader

	// Connections by scope
	mu          sync.RWMutex
	connections map[string]map[*Client]struct{} // scope_id -> clients

	// Scope subscriptions
	subscriptions map[string]func() // scope_id -> cancel func

	register   chan *Client
	unregister chan *Client
	shutdown   chan struct{}

	closing      atomic.Bool
	shutdownOnce sync.Once
}

// NewHub creates a new WebSocket hub.
func NewHub(svc *presence.Service, eventBus events.EventBus, cfg *config.Config) *Hub {
	debouncer := presence.NewDebouncer(svc, eventBus, presence.DebouncerConfig{
		DisconnectDelay: cfg.DisconnectDebounceDelay,
		JobTTL:          cfg.DebounceJobTTL,
		PollInterval:    cfg.DebouncerPollInterval,
		FlappingWindow:  cfg.FlappingWindow,
		FlappingThreshold: cfg.FlappingThreshold,
		OfflineDelay:    cfg.OfflineDelay,
	})

	// Build allowed origins map for O(1) lookup
	allowedOrigins := make(map[string]struct{}, len(cfg.CORSAllowedOrigins))
	for _, origin := range cfg.CORSAllowedOrigins {
		allowedOrigins[strings.ToLower(origin)] = struct{}{}
	}

	h := &Hub{
		svc:           svc,
		EventBus:      eventBus,
		cfg:           cfg,
		validator:     auth.NewValidator(cfg.JWTSecret),
		debouncer:     debouncer,
		connections:   make(map[string]map[*Client]struct{}),
		subscriptions: make(map[string]func()),
		register:      make(chan *Client, maxInt(1, cfg.MaxPendingEvents)),
		unregister:    make(chan *Client, maxInt(1, cfg.MaxPendingEvents)),
		shutdown:      make(chan struct{}),
	}

	// Configure WebSocket upgrader with CORS
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return h.checkOrigin(r, allowedOrigins)
		},
	}

	return h
}

// checkOrigin validates the request origin against allowed origins.
func (h *Hub) checkOrigin(r *http.Request, allowedOrigins map[string]struct{}) bool {
	// In development mode with CORSAllowAll, accept everything
	if h.cfg.CORSAllowAll {
		if h.cfg.IsProduction() {
			log.Warn().Msg("CORS_ALLOW_ALL is enabled in production - this is insecure!")
		}
		return true
	}

	// Get the Origin header
	origin := r.Header.Get("Origin")
	if origin == "" {
		// No origin header - likely same-origin or non-browser client
		// Allow for backwards compatibility, but log in production
		if h.cfg.IsProduction() {
			log.Debug().Str("remote_addr", r.RemoteAddr).Msg("WebSocket connection without Origin header")
		}
		return true
	}

	// Parse and normalize the origin
	parsedOrigin, err := url.Parse(origin)
	if err != nil {
		log.Warn().Str("origin", origin).Err(err).Msg("invalid origin header")
		return false
	}

	// Build normalized origin (scheme + host)
	normalizedOrigin := strings.ToLower(parsedOrigin.Scheme + "://" + parsedOrigin.Host)

	// Check against allowed origins
	if _, ok := allowedOrigins[normalizedOrigin]; ok {
		return true
	}

	// Check for wildcard subdomain patterns (e.g., "*.example.com")
	for allowed := range allowedOrigins {
		if strings.HasPrefix(allowed, "*.") {
			// Extract the base domain
			baseDomain := allowed[1:] // Remove the "*" to get ".example.com"
			if strings.HasSuffix(normalizedOrigin, baseDomain) {
				return true
			}
		}
	}

	log.Warn().
		Str("origin", origin).
		Str("normalized", normalizedOrigin).
		Msg("WebSocket connection rejected: origin not in allowed list")
	return false
}

// Run starts the hub's main event loop.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.shutdown:
			return

		case client := <-h.register:
			h.addClient(ctx, client)

		case client := <-h.unregister:
			h.removeClient(ctx, client)
		}
	}
}

// Shutdown gracefully shuts down the hub.
func (h *Hub) Shutdown() {
	if !h.closing.CompareAndSwap(false, true) {
		return
	}

	var cancels []func()
	var clients []*Client

	h.mu.Lock()
	for _, cancel := range h.subscriptions {
		cancels = append(cancels, cancel)
	}
	for _, scopeClients := range h.connections {
		for client := range scopeClients {
			clients = append(clients, client)
		}
	}
	// Clear maps to prevent further broadcasts/subscription management.
	h.subscriptions = make(map[string]func())
	h.connections = make(map[string]map[*Client]struct{})
	h.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	for _, client := range clients {
		client.Close()
	}

	h.shutdownOnce.Do(func() { close(h.shutdown) })
}

// HandleWebSocket handles WebSocket upgrade and connection setup.
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	if h.closing.Load() {
		http.Error(w, "server shutting down", http.StatusServiceUnavailable)
		return
	}

	// Validate JWT
	claims, err := h.validator.ValidateRequest(r)
	if err != nil {
		log.Warn().Err(err).Msg("websocket auth failed")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Get and validate scope_id
	scopeID := r.URL.Query().Get("scope_id")
	if scopeID == "" {
		http.Error(w, "scope_id required", http.StatusBadRequest)
		return
	}

	// Get optional parameters
	deviceID := r.URL.Query().Get("device_id")
	deviceType := r.URL.Query().Get("device_type") // "desktop", "mobile", "tablet"
	invisible := r.URL.Query().Get("invisible") == "true"

	// Verify scope access
	if !claims.HasScopeAccess(scopeID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Enforce per-user connection cap per scope (pre-upgrade).
	if h.cfg.MaxConnsPerUser > 0 {
		h.mu.RLock()
		existing := 0
		for c := range h.connections[scopeID] {
			if c.userID == claims.UserID {
				existing++
			}
		}
		h.mu.RUnlock()
		if existing >= h.cfg.MaxConnsPerUser {
			http.Error(w, "too many connections", http.StatusTooManyRequests)
			return
		}
	}

	// Upgrade connection
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("websocket upgrade failed")
		return
	}

	// Create client
	sessionID := uuid.New().String()
	client := NewClient(h, conn, claims.UserID, sessionID, scopeID, deviceID, deviceType, invisible, h.cfg)

	// Register
	select {
	case h.register <- client:
	case <-h.shutdown:
		client.Close()
		return
	default:
		// Hub overloaded; fail fast rather than blocking upgrade handler.
		client.Close()
		http.Error(w, "server busy", http.StatusServiceUnavailable)
		return
	}

	// Start client goroutines
	go client.WritePump()
	go client.ReadPump()
}

func (h *Hub) addClient(ctx context.Context, client *Client) {
	if h.closing.Load() {
		client.Close()
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	scopeID := client.scopeID

	// Add to connections
	if h.connections[scopeID] == nil {
		h.connections[scopeID] = make(map[*Client]struct{})
	}
	h.connections[scopeID][client] = struct{}{}

	// Record metrics
	metrics.Global().IncConnections(scopeID)

	// Start scope subscription if needed
	if _, exists := h.subscriptions[scopeID]; !exists {
		h.startSubscription(ctx, scopeID)
	}

	log.Info().
		Str("scope_id", scopeID).
		Str("user_id", client.userID).
		Str("session_id", client.sessionID).
		Msg("client connected")
}

func (h *Hub) removeClient(ctx context.Context, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	scopeID := client.scopeID

	if clients, ok := h.connections[scopeID]; ok {
		if _, exists := clients[client]; exists {
			delete(clients, client)

			// Record metrics (check if normal close via client)
			metrics.Global().DecConnections(scopeID, client.IsNormalClose())

			client.Close()

			// If no more clients in scope, stop subscription
			if len(clients) == 0 {
				delete(h.connections, scopeID)
				if cancel, ok := h.subscriptions[scopeID]; ok {
					cancel()
					delete(h.subscriptions, scopeID)
				}
			}
		}
	}

	log.Info().
		Str("scope_id", scopeID).
		Str("user_id", client.userID).
		Str("session_id", client.sessionID).
		Msg("client disconnected")
}

func (h *Hub) startSubscription(ctx context.Context, scopeID string) {
	eventCh, cancel, err := h.EventBus.Subscribe(ctx, scopeID)
	if err != nil {
		log.Error().Err(err).Str("scope_id", scopeID).Msg("failed to subscribe")
		return
	}

	h.subscriptions[scopeID] = cancel

	go func() {
		for event := range eventCh {
			h.broadcastToScope(scopeID, event)
		}
	}()
}

func (h *Hub) broadcastToScope(scopeID string, event events.PresenceEvent) {
	if h.closing.Load() {
		return
	}

	// Marshal once for all clients (efficiency optimization)
	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	// Record event consumption
	metrics.Global().IncEventConsumed(event.Type)

	h.mu.RLock()
	clientsMap := h.connections[scopeID]
	if len(clientsMap) == 0 {
		h.mu.RUnlock()
		return
	}
	clients := make([]*Client, 0, len(clientsMap))
	for client := range clientsMap {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	sentCount := 0
	for _, client := range clients {
		// Filter: Only send event if:
		// 1. It's about the client's own user (always deliver own events)
		// 2. The client has subscribed to this user's presence
		isOwnEvent := client.userID == event.UserID
		isSubscribed := client.IsSubscribedTo(event.UserID)

		if !isOwnEvent && !isSubscribed {
			continue // Skip - client didn't subscribe to this user
		}

		if client.TrySend(data) {
			sentCount++
		} else {
			// Slow consumer, drop event
			metrics.Global().IncEventDropped()
			if h.cfg.DisconnectSlowConsumers {
				client.Close()
			}
		}
	}

	log.Debug().
		Str("scope_id", scopeID).
		Str("event_type", event.Type).
		Str("event_user_id", event.UserID).
		Int("total_clients", len(clients)).
		Int("sent_to", sentCount).
		Msg("broadcast presence event")
}

// PresenceService returns the presence service for client use.
func (h *Hub) PresenceService() *presence.Service {
	return h.svc
}

// Debouncer returns the debouncer for scheduling disconnect jobs.
func (h *Hub) Debouncer() *presence.Debouncer {
	return h.debouncer
}

// Unregister removes a client from the hub.
func (h *Hub) Unregister(client *Client) {
	if h.closing.Load() {
		return
	}
	select {
	case h.unregister <- client:
	case <-h.shutdown:
		return
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// HeartbeatRateLimit returns the minimum time between heartbeats.
func (h *Hub) HeartbeatRateLimit() time.Duration {
	return h.cfg.HeartbeatRateLimit
}

// ConnectionCount returns the number of active connections.
func (h *Hub) ConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	count := 0
	for _, clients := range h.connections {
		count += len(clients)
	}
	return count
}

// ScopeConnectionCount returns the number of connections for a scope.
func (h *Hub) ScopeConnectionCount(scopeID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return len(h.connections[scopeID])
}
