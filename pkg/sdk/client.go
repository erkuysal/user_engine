// Package sdk provides a client for interacting with presence services remotely.
// Use this when running presence as standalone services.
package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/userengine/presence/pkg/events"

	"github.com/gorilla/websocket"
)

// ReconnectConfig configures the reconnection behavior with exponential backoff.
type ReconnectConfig struct {
	// MinDelay is the minimum delay before reconnecting.
	// Default: 1 second
	MinDelay time.Duration

	// MaxDelay is the maximum delay between reconnection attempts.
	// Default: 30 seconds
	MaxDelay time.Duration

	// MaxAttempts is the maximum number of reconnection attempts.
	// 0 means unlimited attempts.
	// Default: 0 (unlimited)
	MaxAttempts int

	// Multiplier is the factor by which the delay increases after each attempt.
	// Default: 2.0
	Multiplier float64

	// JitterFactor is the maximum random factor applied to the delay (0.0-1.0).
	// Default: 0.3 (30% jitter)
	JitterFactor float64
}

// DefaultReconnectConfig returns the default reconnection configuration.
func DefaultReconnectConfig() ReconnectConfig {
	return ReconnectConfig{
		MinDelay:     1 * time.Second,
		MaxDelay:     30 * time.Second,
		MaxAttempts:  0, // Unlimited
		Multiplier:   2.0,
		JitterFactor: 0.3,
	}
}

// calculateDelay computes the next reconnection delay with jitter.
func (rc ReconnectConfig) calculateDelay(attempt int) time.Duration {
	// Exponential backoff: minDelay * (multiplier ^ attempt)
	delay := float64(rc.MinDelay) * math.Pow(rc.Multiplier, float64(attempt))

	// Cap at max delay
	if delay > float64(rc.MaxDelay) {
		delay = float64(rc.MaxDelay)
	}

	// Apply jitter: delay * (1 - jitter + random * 2 * jitter)
	// This creates a range of [delay * (1-jitter), delay * (1+jitter)]
	jitter := (rand.Float64()*2 - 1) * rc.JitterFactor
	delay = delay * (1 + jitter)

	return time.Duration(delay)
}

// Client provides access to presence services.
type Client struct {
	httpBase        string
	wsBase          string
	token           string
	http            *http.Client
	reconnectConfig ReconnectConfig
}

// ClientOption configures the client.
type ClientOption func(*Client)

// WithReconnectConfig sets the reconnection configuration.
func WithReconnectConfig(cfg ReconnectConfig) ClientOption {
	return func(c *Client) {
		c.reconnectConfig = cfg
	}
}

// WithHTTPTimeout sets the HTTP client timeout.
func WithHTTPTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.http.Timeout = timeout
	}
}

// NewClient creates a new presence client.
func NewClient(httpBase, wsBase, token string, opts ...ClientOption) *Client {
	c := &Client{
		httpBase: httpBase,
		wsBase:   wsBase,
		token:    token,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
		reconnectConfig: DefaultReconnectConfig(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// LookupRequest is the request for looking up user presence.
type LookupRequest struct {
	ScopeID string   `json:"scope_id"`
	UserIDs []string `json:"user_ids"`
}

// LookupResponse is the response from looking up user presence.
type LookupResponse struct {
	Statuses map[string]string `json:"statuses"`
}

// Lookup checks the presence status of specific users.
func (c *Client) Lookup(ctx context.Context, scopeID string, userIDs []string) (map[string]string, error) {
	req := LookupRequest{
		ScopeID: scopeID,
		UserIDs: userIDs,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.httpBase+"/presence/lookup", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("lookup failed: %d %s", resp.StatusCode, string(bodyBytes))
	}

	var result LookupResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Statuses, nil
}

// OnlineResponse is the response from getting online users.
type OnlineResponse struct {
	Users  []string `json:"users"`
	Cursor string   `json:"cursor"`
}

// GetOnline returns online users for a scope with pagination.
func (c *Client) GetOnline(ctx context.Context, scopeID, cursor string, count int) (*OnlineResponse, error) {
	u, _ := url.Parse(c.httpBase + "/presence/online")
	q := u.Query()
	q.Set("scope_id", scopeID)
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	if count > 0 {
		q.Set("count", fmt.Sprintf("%d", count))
	}
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get online failed: %d %s", resp.StatusCode, string(bodyBytes))
	}

	var result OnlineResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetAllOnline returns all online users for a scope (handles pagination internally).
func (c *Client) GetAllOnline(ctx context.Context, scopeID string) ([]string, error) {
	var allUsers []string
	cursor := ""

	for {
		resp, err := c.GetOnline(ctx, scopeID, cursor, 100)
		if err != nil {
			return nil, err
		}

		allUsers = append(allUsers, resp.Users...)

		if resp.Cursor == "0" || resp.Cursor == "" {
			break
		}
		cursor = resp.Cursor
	}

	return allUsers, nil
}

// IsOnline checks if a single user is online.
func (c *Client) IsOnline(ctx context.Context, scopeID, userID string) (bool, error) {
	statuses, err := c.Lookup(ctx, scopeID, []string{userID})
	if err != nil {
		return false, err
	}
	return statuses[userID] == "online", nil
}

// ConnectionState represents the current state of the connection.
type ConnectionState int32

const (
	// StateDisconnected means the connection is not active.
	StateDisconnected ConnectionState = iota
	// StateConnecting means a connection attempt is in progress.
	StateConnecting
	// StateConnected means the connection is active and healthy.
	StateConnected
	// StateReconnecting means the connection was lost and reconnection is in progress.
	StateReconnecting
	// StateClosed means the connection has been permanently closed.
	StateClosed
)

func (s ConnectionState) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateReconnecting:
		return "reconnecting"
	case StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Connection represents a WebSocket connection to presence events.
type Connection struct {
	client          *Client
	conn            *websocket.Conn
	scopeID         string
	deviceID        string
	eventCh         chan events.PresenceEvent
	done            chan struct{}
	closeOnce       sync.Once
	lastSeenVersion map[string]int64
	mu              sync.Mutex
	connMu          sync.Mutex // Protects conn field

	// State tracking
	state          int32 // atomic ConnectionState
	reconnectCount int32 // atomic counter
	autoReconnect  bool

	// Callbacks
	OnEvent             func(event events.PresenceEvent)
	OnReconnectRequired func(reason string)
	OnError             func(err error)
	OnStateChange       func(state ConnectionState)
	OnReconnectAttempt  func(attempt int, delay time.Duration)
}

// ConnectOption configures a connection.
type ConnectOption func(*Connection)

// WithAutoReconnect enables automatic reconnection with exponential backoff.
func WithAutoReconnect(enabled bool) ConnectOption {
	return func(c *Connection) {
		c.autoReconnect = enabled
	}
}

// WithDeviceID sets the device ID for the connection.
func WithDeviceID(deviceID string) ConnectOption {
	return func(c *Connection) {
		c.deviceID = deviceID
	}
}

// Connect establishes a WebSocket connection to receive presence events.
func (c *Client) Connect(ctx context.Context, scopeID string, opts ...ConnectOption) (*Connection, error) {
	connection := &Connection{
		client:          c,
		scopeID:         scopeID,
		eventCh:         make(chan events.PresenceEvent, 100),
		done:            make(chan struct{}),
		lastSeenVersion: make(map[string]int64),
		autoReconnect:   true, // Default to auto-reconnect
	}

	for _, opt := range opts {
		opt(connection)
	}

	if err := connection.connect(ctx); err != nil {
		return nil, err
	}

	return connection, nil
}

// connect performs the actual WebSocket connection.
func (c *Connection) connect(ctx context.Context) error {
	c.setState(StateConnecting)

	u, _ := url.Parse(c.client.wsBase + "/ws")
	q := u.Query()
	q.Set("scope_id", c.scopeID)
	q.Set("token", c.client.token)
	if c.deviceID != "" {
		q.Set("device_id", c.deviceID)
	}
	u.RawQuery = q.Encode()

	// Switch to ws:// or wss://
	if u.Scheme == "http" {
		u.Scheme = "ws"
	} else if u.Scheme == "https" {
		u.Scheme = "wss"
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		c.setState(StateDisconnected)
		return fmt.Errorf("websocket dial: %w", err)
	}

	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()

	c.setState(StateConnected)
	atomic.StoreInt32(&c.reconnectCount, 0) // Reset on successful connect

	// Start reader
	go c.readPump()

	// Start heartbeat
	go c.heartbeatPump()

	return nil
}

// reconnectWithBackoff attempts to reconnect with exponential backoff and jitter.
func (c *Connection) reconnectWithBackoff(ctx context.Context) {
	cfg := c.client.reconnectConfig
	attempt := 0

	for {
		select {
		case <-c.done:
			return
		case <-ctx.Done():
			return
		default:
		}

		// Check max attempts
		if cfg.MaxAttempts > 0 && attempt >= cfg.MaxAttempts {
			if c.OnError != nil {
				c.OnError(fmt.Errorf("max reconnection attempts (%d) exceeded", cfg.MaxAttempts))
			}
			c.setState(StateDisconnected)
			return
		}

		c.setState(StateReconnecting)
		atomic.AddInt32(&c.reconnectCount, 1)

		// Calculate delay with jitter
		delay := cfg.calculateDelay(attempt)

		if c.OnReconnectAttempt != nil {
			c.OnReconnectAttempt(attempt+1, delay)
		}

		// Wait before attempting
		select {
		case <-c.done:
			return
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		// Attempt reconnection
		if err := c.connect(ctx); err != nil {
			attempt++
			if c.OnError != nil {
				c.OnError(fmt.Errorf("reconnection attempt %d failed: %w", attempt, err))
			}
			continue
		}

		// Success
		return
	}
}

// State returns the current connection state.
func (c *Connection) State() ConnectionState {
	return ConnectionState(atomic.LoadInt32(&c.state))
}

// ReconnectCount returns the number of reconnection attempts since last successful connect.
func (c *Connection) ReconnectCount() int {
	return int(atomic.LoadInt32(&c.reconnectCount))
}

func (c *Connection) setState(state ConnectionState) {
	old := ConnectionState(atomic.SwapInt32(&c.state, int32(state)))
	if old != state && c.OnStateChange != nil {
		c.OnStateChange(state)
	}
}

// Events returns a channel of presence events.
// Events are already deduplicated by version.
func (c *Connection) Events() <-chan events.PresenceEvent {
	return c.eventCh
}

// Close closes the connection permanently (no reconnection).
func (c *Connection) Close() error {
	c.closeOnce.Do(func() {
		c.setState(StateClosed)
		close(c.done)
		close(c.eventCh)
		c.connMu.Lock()
		if c.conn != nil {
			c.conn.Close()
		}
		c.connMu.Unlock()
	})
	return nil
}

// Disconnect closes the connection with a normal close code.
// Use this for logout scenarios where you want immediate offline transition.
func (c *Connection) Disconnect() error {
	c.connMu.Lock()
	conn := c.conn
	c.connMu.Unlock()

	if conn != nil {
		// Send close frame with normal closure code
		conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "logout"),
			time.Now().Add(time.Second),
		)
	}
	return c.Close()
}

// ScopeID returns the scope this connection is subscribed to.
func (c *Connection) ScopeID() string {
	return c.scopeID
}

func (c *Connection) readPump() {
	defer func() {
		c.connMu.Lock()
		if c.conn != nil {
			c.conn.Close()
		}
		c.connMu.Unlock()
	}()

	for {
		select {
		case <-c.done:
			return
		default:
		}

		c.connMu.Lock()
		conn := c.conn
		c.connMu.Unlock()

		if conn == nil {
			return
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			// Check if we should reconnect
			select {
			case <-c.done:
				// Connection is being closed, don't reconnect
				return
			default:
				c.setState(StateDisconnected)
				if c.OnError != nil {
					c.OnError(err)
				}

				// Trigger reconnection if enabled
				if c.autoReconnect && c.State() != StateClosed {
					go c.reconnectWithBackoff(context.Background())
				}
				return
			}
		}

		// Try to parse as presence event
		var event events.PresenceEvent
		if err := json.Unmarshal(message, &event); err != nil {
			continue
		}

		// Check for reconnect_required
		if event.Type == "" {
			var reconnect events.ReconnectRequiredMessage
			if err := json.Unmarshal(message, &reconnect); err == nil && reconnect.Type == "reconnect_required" {
				if c.OnReconnectRequired != nil {
					c.OnReconnectRequired(reconnect.Reason)
				}

				// Server requested reconnect, trigger it
				if c.autoReconnect {
					go c.reconnectWithBackoff(context.Background())
				}
				return
			}
			continue
		}

		// Deduplicate by version
		c.mu.Lock()
		lastVersion := c.lastSeenVersion[event.UserID]
		if event.Version <= lastVersion {
			c.mu.Unlock()
			continue // Skip stale/duplicate event
		}
		c.lastSeenVersion[event.UserID] = event.Version
		c.mu.Unlock()

		// Deliver event
		if c.OnEvent != nil {
			c.OnEvent(event)
		}

		select {
		case c.eventCh <- event:
		default:
			// Buffer full, drop oldest
		}
	}
}

func (c *Connection) heartbeatPump() {
	// Send heartbeat every 15s with jitter to avoid thundering herd
	baseInterval := 15 * time.Second
	jitter := time.Duration(rand.Int63n(int64(3 * time.Second)))
	ticker := time.NewTicker(baseInterval + jitter)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			// Only send heartbeat if connected
			if c.State() != StateConnected {
				continue
			}

			c.connMu.Lock()
			conn := c.conn
			c.connMu.Unlock()

			if conn == nil {
				return
			}

			msg := map[string]string{"type": "heartbeat"}
			data, _ := json.Marshal(msg)
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				// Error will be handled by readPump
				return
			}

			// Re-jitter the next interval to spread out heartbeats
			ticker.Reset(baseInterval + time.Duration(rand.Int63n(int64(3*time.Second))))
		}
	}
}

// LastSeenVersion returns the last seen version for a user.
func (c *Connection) LastSeenVersion(userID string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastSeenVersion[userID]
}

// ResetVersions clears all version tracking (call after re-fetching snapshot).
func (c *Connection) ResetVersions() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastSeenVersion = make(map[string]int64)
}
