package ws

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/userengine/presence/pkg/config"
	"github.com/userengine/presence/pkg/events"
	"github.com/userengine/presence/pkg/metrics"
	"github.com/userengine/presence/pkg/presence"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// Client represents a WebSocket connection.
type Client struct {
	hub        *Hub
	conn       *websocket.Conn
	userID     string
	sessionID  string
	scopeID    string
	deviceID   string
	deviceType string
	invisible  bool
	cfg        *config.Config

	send          chan []byte
	done          chan struct{}
	closeOnce     sync.Once
	lastHeartbeat time.Time
	mu            sync.Mutex
	normalClose   bool
	connected     bool

	// subscribedUsers holds user IDs this client wants presence events for (friend subscriptions)
	subscribedUsers map[string]struct{}
}

// ClientMessage represents an incoming message from a client.
type ClientMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// SubscribeFriendsPayload is the payload for subscribe_friends messages.
type SubscribeFriendsPayload struct {
	UserIDs []string `json:"user_ids"`
}

// NewClient creates a new WebSocket client.
func NewClient(hub *Hub, conn *websocket.Conn, userID, sessionID, scopeID, deviceID, deviceType string, invisible bool, cfg *config.Config) *Client {
	return &Client{
		hub:        hub,
		conn:       conn,
		userID:     userID,
		sessionID:  sessionID,
		scopeID:    scopeID,
		deviceID:   deviceID,
		deviceType: deviceType,
		invisible:  invisible,
		cfg:        cfg,
		send:       make(chan []byte, cfg.SlowConsumerBuffer),
		done:       make(chan struct{}),
	}
}

// ReadPump reads messages from the WebSocket connection.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.scheduleDisconnect()
	}()

	c.conn.SetReadLimit(c.cfg.MaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(c.cfg.PongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(c.cfg.PongWait))
		return nil
	})

	// Perform initial connect
	if err := c.connect(); err != nil {
		log.Error().Err(err).
			Str("user_id", c.userID).
			Str("session_id", c.sessionID).
			Msg("initial connect failed")
		return
	}
	c.mu.Lock()
	c.connected = true
	c.mu.Unlock()

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			isNormal := false
			if ce, ok := err.(*websocket.CloseError); ok {
				if ce.Code == 1000 || ce.Code == websocket.CloseNormalClosure || ce.Code == 1001 || ce.Code == websocket.CloseGoingAway {
					isNormal = true
				}
			}

			if isNormal {
				c.mu.Lock()
				c.normalClose = true
				c.mu.Unlock()
			}

			// Still call IsUnexpected for others to keep logs clean
			if !isNormal && websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
				log.Error().Err(err).Str("session_id", c.sessionID).Msg("read error")
			}
			return
		}

		var msg ClientMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "heartbeat":
			c.handleHeartbeat()
		case "subscribe_friends":
			c.handleSubscribeFriends(msg.Payload)
		}
	}
}

// ... (WritePump and others unchanged) ...

func (c *Client) scheduleDisconnect() {
	c.mu.Lock()
	connected := c.connected
	c.mu.Unlock()

	// If we never connected successfully, don't schedule disconnect work.
	if !connected {
		return
	}

	c.mu.Lock()
	isNormalClose := c.normalClose
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	debouncer := c.hub.Debouncer()

	_, err := debouncer.ScheduleDisconnect(ctx, c.scopeID, c.userID, c.sessionID)
	if err != nil {
		log.Error().Err(err).
			Str("session_id", c.sessionID).
			Bool("normal_close", isNormalClose).
			Msg("failed to schedule disconnect")
	}
}

// WritePump sends messages to the WebSocket connection.
func (c *Client) WritePump() {
	ticker := time.NewTicker(c.cfg.PingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case <-c.done:
			return

		case message := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout))
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Close closes the client connection.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

// UserID returns the client's user ID.
func (c *Client) UserID() string {
	return c.userID
}

// SessionID returns the client's session ID.
func (c *Client) SessionID() string {
	return c.sessionID
}

// ScopeID returns the client's scope ID.
func (c *Client) ScopeID() string {
	return c.scopeID
}

// IsNormalClose returns true if the connection was closed normally (logout).
func (c *Client) IsNormalClose() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.normalClose
}

func (c *Client) connect() error {
	ctx := context.Background()
	svc := c.hub.PresenceService()

	// Cancel any pending disconnect jobs for this user/session
	debouncer := c.hub.Debouncer()
	if _, err := debouncer.CancelDisconnect(ctx, c.scopeID, c.userID, c.sessionID); err != nil {
		log.Warn().Err(err).Msg("failed to cancel pending disconnect")
	}

	// Register session
	meta := presence.SessionMeta{
		UserID:      c.userID,
		SessionID:   c.sessionID,
		ScopeID:     c.scopeID,
		DeviceID:    c.deviceID,
		DeviceType:  c.deviceType,
		Invisible:   c.invisible,
		ConnectedAt: time.Now().UTC(),
	}

	result, err := svc.Connect(ctx, meta)
	if err != nil {
		return err
	}

	// If user came online, publish event (unless invisible)
	if result.Transition == "online" && !c.invisible {
		event := events.PresenceEvent{
			EventID:    events.NewEventID(),
			ScopeID:    c.scopeID,
			Type:       events.EventTypeUserOnline,
			UserID:     c.userID,
			Version:    result.Version,
			TabCount:   result.SessionCount,
			OccurredAt: time.Now().UTC(),
			Source:     "gateway",
			DeviceID:   c.deviceID,
		}

		if err := c.hub.EventBus.Publish(ctx, c.scopeID, event); err != nil {
			log.Error().Err(err).Msg("failed to publish online event")
		}
	} else if result.Transition == "invisible" {
		log.Debug().
			Str("user_id", c.userID).
			Str("session_id", c.sessionID).
			Msg("user connected in invisible mode - no event published")
	} else if result.SessionCount > 0 && !c.invisible {
		event := events.PresenceEvent{
			EventID:    events.NewEventID(),
			ScopeID:    c.scopeID,
			Type:       events.EventTypeUserTabs,
			UserID:     c.userID,
			Version:    result.Version,
			TabCount:   result.SessionCount,
			OccurredAt: time.Now().UTC(),
			Source:     "gateway",
			DeviceID:   c.deviceID,
		}

		if err := c.hub.EventBus.Publish(ctx, c.scopeID, event); err != nil {
			log.Error().Err(err).Msg("failed to publish tab count event")
		}
	}

	c.mu.Lock()
	c.lastHeartbeat = time.Now()
	c.mu.Unlock()

	log.Debug().
		Str("user_id", c.userID).
		Str("session_id", c.sessionID).
		Str("transition", result.Transition).
		Msg("session connected")

	return nil
}

func (c *Client) handleHeartbeat() {
	now := time.Now()
	c.mu.Lock()
	lastHB := c.lastHeartbeat
	timeSinceLast := now.Sub(lastHB)
	c.mu.Unlock()

	// Rate limit heartbeats
	if timeSinceLast < c.hub.HeartbeatRateLimit() {
		metrics.Global().IncHeartbeat(false) // rejected
		log.Warn().
			Str("session_id", c.sessionID).
			Str("user_id", c.userID).
			Dur("since_last", timeSinceLast).
			Dur("rate_limit", c.hub.HeartbeatRateLimit()).
			Msg("heartbeat rejected (rate limited)")
		return
	}

	ctx := context.Background()
	svc := c.hub.PresenceService()

	err := svc.Heartbeat(ctx, c.scopeID, c.sessionID)
	if err != nil {
		metrics.Global().IncHeartbeat(false) // rejected
		if err == presence.ErrSessionUnknown {
			// Session expired, send reconnect_required and close with application code
			log.Error().
				Str("session_id", c.sessionID).
				Str("user_id", c.userID).
				Dur("since_last_hb", timeSinceLast).
				Msg("session expired; reconnect required")
			c.sendReconnectRequired("session_expired")
			c.conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(CloseCodeSessionExpired, "session_expired"),
				time.Now().Add(time.Second),
			)
			c.conn.Close()
			return
		}
		log.Error().
			Err(err).
			Str("session_id", c.sessionID).
			Str("user_id", c.userID).
			Msg("heartbeat failed")
		return
	}

	metrics.Global().IncHeartbeat(true) // accepted

	c.mu.Lock()
	c.lastHeartbeat = time.Now()
	c.mu.Unlock()

	log.Debug().
		Str("session_id", c.sessionID).
		Str("user_id", c.userID).
		Dur("interval", timeSinceLast).
		Msg("heartbeat ok")
}

func (c *Client) sendReconnectRequired(reason string) {
	msg := events.NewReconnectRequired(reason)
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	select {
	case c.send <- data:
	default:
	}

	log.Info().
		Str("session_id", c.sessionID).
		Str("reason", reason).
		Msg("sent reconnect_required")
}

// handleSubscribeFriends handles friend subscription requests.
// Clients send this to subscribe to presence events for specific user IDs (friends).
func (c *Client) handleSubscribeFriends(payload json.RawMessage) {
	var sub SubscribeFriendsPayload
	if err := json.Unmarshal(payload, &sub); err != nil {
		log.Warn().
			Err(err).
			Str("session_id", c.sessionID).
			Msg("invalid subscribe_friends payload")
		return
	}

	// Limit subscriptions to prevent abuse
	if c.cfg.MaxFriendSubscriptions > 0 && len(sub.UserIDs) > c.cfg.MaxFriendSubscriptions {
		sub.UserIDs = sub.UserIDs[:c.cfg.MaxFriendSubscriptions]
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Replace current subscriptions (additive would require unsubscribe logic)
	c.subscribedUsers = make(map[string]struct{}, len(sub.UserIDs))
	for _, userID := range sub.UserIDs {
		c.subscribedUsers[userID] = struct{}{}
	}

	log.Debug().
		Str("session_id", c.sessionID).
		Str("user_id", c.userID).
		Int("friend_count", len(c.subscribedUsers)).
		Msg("subscribed to friend presence events")
}

// IsSubscribedTo returns true if the client is subscribed to events for the given user ID.
func (c *Client) IsSubscribedTo(userID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.subscribedUsers[userID]
	return ok
}

// TrySend attempts to enqueue a message to this client without blocking.
// Returns true if enqueued, false if the client is closed or backpressured.
func (c *Client) TrySend(data []byte) bool {
	select {
	case <-c.done:
		return false
	default:
	}

	select {
	case c.send <- data:
		return true
	default:
		return false
	}
}
