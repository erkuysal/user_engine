package test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/userengine/presence/pkg/events"
)

func TestEventTypes(t *testing.T) {
	if events.EventTypeUserOnline != "user_online" {
		t.Errorf("EventTypeUserOnline = %q, want %q", events.EventTypeUserOnline, "user_online")
	}
	if events.EventTypeUserOffline != "user_offline" {
		t.Errorf("EventTypeUserOffline = %q, want %q", events.EventTypeUserOffline, "user_offline")
	}
	if events.EventTypeUserTabs != "user_tabs" {
		t.Errorf("EventTypeUserTabs = %q, want %q", events.EventTypeUserTabs, "user_tabs")
	}
}

func TestNewEventID(t *testing.T) {
	id1 := events.NewEventID()
	id2 := events.NewEventID()

	if id1 == "" {
		t.Error("NewEventID() returned empty string")
	}

	if id1 == id2 {
		t.Error("NewEventID() should return unique IDs")
	}
}

func TestPresenceEvent_JSON(t *testing.T) {
	event := events.PresenceEvent{
		EventID:    "event-123",
		ScopeID:    "scope-1",
		Type:       events.EventTypeUserOnline,
		UserID:     "user-456",
		Version:    42,
		TabCount:   2,
		OccurredAt: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Source:     "gateway",
		DeviceID:   "device-789",
	}

	// Marshal
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Unmarshal
	var decoded events.PresenceEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.EventID != event.EventID {
		t.Errorf("EventID = %q, want %q", decoded.EventID, event.EventID)
	}
	if decoded.Type != event.Type {
		t.Errorf("Type = %q, want %q", decoded.Type, event.Type)
	}
	if decoded.UserID != event.UserID {
		t.Errorf("UserID = %q, want %q", decoded.UserID, event.UserID)
	}
	if decoded.Version != event.Version {
		t.Errorf("Version = %d, want %d", decoded.Version, event.Version)
	}
	if decoded.TabCount != event.TabCount {
		t.Errorf("TabCount = %d, want %d", decoded.TabCount, event.TabCount)
	}
}

func TestNewReconnectRequired(t *testing.T) {
	msg := events.NewReconnectRequired("session_expired")

	if msg.Type != "reconnect_required" {
		t.Errorf("Type = %q, want %q", msg.Type, "reconnect_required")
	}
	if msg.Reason != "session_expired" {
		t.Errorf("Reason = %q, want %q", msg.Reason, "session_expired")
	}
}

func TestReconnectRequiredMessage_JSON(t *testing.T) {
	msg := events.NewReconnectRequired("server_restart")

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded events.ReconnectRequiredMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Type != "reconnect_required" {
		t.Errorf("Type = %q, want %q", decoded.Type, "reconnect_required")
	}
	if decoded.Reason != "server_restart" {
		t.Errorf("Reason = %q, want %q", decoded.Reason, "server_restart")
	}
}
