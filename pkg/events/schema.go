package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Event types
const (
	EventTypeUserOnline  = "user_online"
	EventTypeUserOffline = "user_offline"
	EventTypeUserTabs    = "user_tabs"
)

// PresenceEvent is the canonical presence event schema.
type PresenceEvent struct {
	// EventID is a unique identifier (UUID v4) for deduplication.
	EventID string `json:"event_id"`

	// ScopeID identifies the tenant/workspace.
	ScopeID string `json:"scope_id"`

	// Type is the event type: user_online, user_offline, or user_tabs.
	Type string `json:"type"`

	// UserID is the user this event is about.
	UserID string `json:"user_id"`

	// Version is a monotonic counter per user for ordering/deduplication.
	// Clients should apply events only if version > last_seen_version.
	Version int64 `json:"version"`

	// TabCount is the number of active sessions (tabs) for the user.
	TabCount int64 `json:"tab_count,omitempty"`

	// OccurredAt is when the event occurred (RFC3339 UTC).
	OccurredAt time.Time `json:"occurred_at"`

	// Source identifies the emitter for debugging (gateway/sweeper/debouncer).
	Source string `json:"source,omitempty"`

	// DeviceID is a stable client identifier if provided by the client.
	DeviceID string `json:"device_id,omitempty"`
}

// NewEventID generates a new event ID.
func NewEventID() string {
	return uuid.New().String()
}

// MarshalJSON implements custom JSON marshaling for RFC3339 time.
func (e PresenceEvent) MarshalJSON() ([]byte, error) {
	type Alias PresenceEvent
	return json.Marshal(&struct {
		OccurredAt string `json:"occurred_at"`
		*Alias
	}{
		OccurredAt: e.OccurredAt.Format(time.RFC3339),
		Alias:      (*Alias)(&e),
	})
}

// UnmarshalJSON implements custom JSON unmarshaling for RFC3339 time.
func (e *PresenceEvent) UnmarshalJSON(data []byte) error {
	type Alias PresenceEvent
	aux := &struct {
		OccurredAt string `json:"occurred_at"`
		*Alias
	}{
		Alias: (*Alias)(e),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if aux.OccurredAt != "" {
		t, err := time.Parse(time.RFC3339, aux.OccurredAt)
		if err != nil {
			return err
		}
		e.OccurredAt = t
	}

	return nil
}

// ReconnectRequiredMessage is sent to clients when their session has expired.
type ReconnectRequiredMessage struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// NewReconnectRequired creates a reconnect required message.
func NewReconnectRequired(reason string) ReconnectRequiredMessage {
	return ReconnectRequiredMessage{
		Type:   "reconnect_required",
		Reason: reason,
	}
}
