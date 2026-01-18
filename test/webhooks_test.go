package test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/userengine/presence/pkg/events"
	"github.com/userengine/presence/pkg/webhooks"
)

func TestWebhookEvent_JSON(t *testing.T) {
	event := webhooks.WebhookEvent{
		ID: "event-123",
		Event: events.PresenceEvent{
			EventID:    "event-123",
			ScopeID:    "scope-1",
			Type:       events.EventTypeUserOnline,
			UserID:     "user-456",
			Version:    42,
			TabCount:   2,
			OccurredAt: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		WebhookURL: "https://example.com/webhook",
		CreatedAt:  time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Attempt:    0,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded webhooks.WebhookEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.ID != event.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, event.ID)
	}
	if decoded.WebhookURL != event.WebhookURL {
		t.Errorf("WebhookURL = %q, want %q", decoded.WebhookURL, event.WebhookURL)
	}
	if decoded.Attempt != event.Attempt {
		t.Errorf("Attempt = %d, want %d", decoded.Attempt, event.Attempt)
	}
	if decoded.Event.UserID != event.Event.UserID {
		t.Errorf("Event.UserID = %q, want %q", decoded.Event.UserID, event.Event.UserID)
	}
}

func TestWebhookEvent_Fields(t *testing.T) {
	presenceEvent := events.PresenceEvent{
		EventID:    "event-123",
		ScopeID:    "scope-1",
		Type:       events.EventTypeUserOnline,
		UserID:     "user-456",
		Version:    42,
		TabCount:   2,
		OccurredAt: time.Now(),
		DeviceID:   "device-789",
	}

	webhookEvent := webhooks.WebhookEvent{
		ID:         presenceEvent.EventID,
		Event:      presenceEvent,
		WebhookURL: "https://example.com/webhook",
		CreatedAt:  time.Now(),
		Attempt:    0,
		Signature:  "abc123",
	}

	if webhookEvent.ID != presenceEvent.EventID {
		t.Errorf("ID = %q, want %q", webhookEvent.ID, presenceEvent.EventID)
	}
	if webhookEvent.Event.Type != presenceEvent.Type {
		t.Errorf("Event.Type = %q, want %q", webhookEvent.Event.Type, presenceEvent.Type)
	}
	if webhookEvent.Signature != "abc123" {
		t.Errorf("Signature = %q, want %q", webhookEvent.Signature, "abc123")
	}
}
