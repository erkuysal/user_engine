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
}

// ClientMessage represents an incoming message from a client.
type ClientMessage struct {
	Type string `json:"type"`
}

// NewClient creates a new WebSocket client.
func NewClient(hub *Hub, conn *websocket.Conn, userID, sessionID, scopeID, deviceID, deviceType string, invisible bool, cfg *config.Config) *Client {
	log.Debug().
		Str("session_id", sessionID).
		Str("device_type", deviceType).
		Bool("invisible", invisible).
		Msg("NewClient created")
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

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			// DEBUG: Extensive closing logic logging
			isNormal := false
			if ce, ok := err.(*websocket.CloseError); ok {
				log.Info().Int("code", ce.Code).Str("text", ce.Text).Msg("WebSocket closed via CloseError")
				if ce.Code == 1000 || ce.Code == websocket.CloseNormalClosure || ce.Code == 1001 || ce.Code == websocket.CloseGoingAway {
					isNormal = true
				}
			} else {
				log.Info().Err(err).Msg("WebSocket read error (not CloseError)")
			}

			if isNormal {
				log.Error().Msg("!!! DETECTED NORMAL CLOSE (1000) - TRIGGERING IMMEDIATE DISCONNECT !!!")
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
		}
	}
}

// ... (WritePump and others unchanged) ...

func (c *Client) scheduleDisconnect() {
	c.mu.Lock()
	isNormalClose := c.normalClose
	c.mu.Unlock()

	ctx := context.Background()
	log.Info().Bool("normal_close", isNormalClose).Str("session_id", c.sessionID).Msg("scheduleDisconnect called")

	// If closed normally (logout), disconnect immediately without debounce
	if isNormalClose {
		log.Info().
			Str("session_id", c.sessionID).
			Str("user_id", c.userID).
			Msg("immediate disconnect (normal close)")

		svc := c.hub.PresenceService()
		result, err := svc.Disconnect(ctx, c.scopeID, c.sessionID, c.userID)
		if err != nil {
			log.Error().Err(err).
				Str("session_id", c.sessionID).
				Msg("failed to disconnect session")
			return
		}

		if result.Transition == "offline" {
			event := events.PresenceEvent{
				EventID:    events.NewEventID(),
				ScopeID:    c.scopeID,
				Type:       events.EventTypeUserOffline,
				UserID:     c.userID,
				Version:    result.Version,
				TabCount:   result.SessionCount,
				OccurredAt: time.Now().UTC(),
				Source:     "gateway",
				DeviceID:   c.deviceID,
			}

			if err := c.hub.EventBus.Publish(ctx, c.scopeID, event); err != nil {
				log.Error().Err(err).Msg("failed to publish offline event")
			}
		} else if result.SessionCount > 0 {
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
		return
	}

	// Abnormal close: schedule debounce
	debouncer := c.hub.Debouncer()

	_, err := debouncer.ScheduleDisconnect(ctx, c.scopeID, c.userID, c.sessionID)
	if err != nil {
		log.Error().Err(err).
			Str("session_id", c.sessionID).
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

		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

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
		close(c.send)
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
	c.mu.Lock()
	timeSinceLast := time.Since(c.lastHeartbeat)
	c.mu.Unlock()

	// Rate limit heartbeats
	if timeSinceLast < c.hub.HeartbeatRateLimit() {
		metrics.Global().IncHeartbeat(false) // rejected
		log.Warn().
			Str("session_id", c.sessionID).
			Dur("since_last", timeSinceLast).
			Msg("heartbeat rate limited")
		return
	}

	ctx := context.Background()
	svc := c.hub.PresenceService()

	err := svc.Heartbeat(ctx, c.scopeID, c.sessionID)
	if err != nil {
		metrics.Global().IncHeartbeat(false) // rejected
		if err == presence.ErrSessionUnknown {
			// Session expired, send reconnect_required and close with application code
			c.sendReconnectRequired("session_expired")
			c.conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(CloseCodeSessionExpired, "session_expired"),
				time.Now().Add(time.Second),
			)
			c.conn.Close()
			return
		}
		log.Error().Err(err).Str("session_id", c.sessionID).Msg("heartbeat failed")
		return
	}

	metrics.Global().IncHeartbeat(true) // accepted

	c.mu.Lock()
	c.lastHeartbeat = time.Now()
	c.mu.Unlock()

	log.Debug().Str("session_id", c.sessionID).Msg("heartbeat ok")
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
