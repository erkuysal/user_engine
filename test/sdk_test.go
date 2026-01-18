package test

import (
	"testing"
	"time"

	"github.com/userengine/presence/pkg/sdk"
)

func TestSDKNewClient(t *testing.T) {
	client := sdk.NewClient("http://localhost:8081", "ws://localhost:8080", "test-token")

	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
}

func TestSDKNewClient_WithOptions(t *testing.T) {
	customConfig := sdk.ReconnectConfig{
		MinDelay:     2 * time.Second,
		MaxDelay:     60 * time.Second,
		MaxAttempts:  10,
		Multiplier:   3.0,
		JitterFactor: 0.5,
	}

	client := sdk.NewClient(
		"http://localhost:8081",
		"ws://localhost:8080",
		"test-token",
		sdk.WithReconnectConfig(customConfig),
		sdk.WithHTTPTimeout(60*time.Second),
	)

	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
}

func TestSDKDefaultReconnectConfig(t *testing.T) {
	cfg := sdk.DefaultReconnectConfig()

	if cfg.MinDelay != 1*time.Second {
		t.Errorf("MinDelay = %v, want %v", cfg.MinDelay, 1*time.Second)
	}

	if cfg.MaxDelay != 30*time.Second {
		t.Errorf("MaxDelay = %v, want %v", cfg.MaxDelay, 30*time.Second)
	}

	if cfg.MaxAttempts != 0 {
		t.Errorf("MaxAttempts = %d, want %d (unlimited)", cfg.MaxAttempts, 0)
	}

	if cfg.Multiplier != 2.0 {
		t.Errorf("Multiplier = %f, want %f", cfg.Multiplier, 2.0)
	}

	if cfg.JitterFactor != 0.3 {
		t.Errorf("JitterFactor = %f, want %f", cfg.JitterFactor, 0.3)
	}
}

func TestSDKConnectionState_String(t *testing.T) {
	tests := []struct {
		state sdk.ConnectionState
		want  string
	}{
		{sdk.StateDisconnected, "disconnected"},
		{sdk.StateConnecting, "connecting"},
		{sdk.StateConnected, "connected"},
		{sdk.StateReconnecting, "reconnecting"},
		{sdk.StateClosed, "closed"},
		{sdk.ConnectionState(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.state.String()
		if got != tt.want {
			t.Errorf("ConnectionState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestSDKLookupRequest_Fields(t *testing.T) {
	req := sdk.LookupRequest{
		ScopeID: "scope1",
		UserIDs: []string{"user1", "user2"},
	}

	if req.ScopeID != "scope1" {
		t.Errorf("ScopeID = %q, want %q", req.ScopeID, "scope1")
	}

	if len(req.UserIDs) != 2 {
		t.Errorf("len(UserIDs) = %d, want 2", len(req.UserIDs))
	}
}

func TestSDKLookupResponse_Fields(t *testing.T) {
	resp := sdk.LookupResponse{
		Statuses: map[string]string{
			"user1": "online",
			"user2": "offline",
		},
	}

	if resp.Statuses["user1"] != "online" {
		t.Errorf("Statuses[user1] = %q, want %q", resp.Statuses["user1"], "online")
	}
}

func TestSDKOnlineResponse_Fields(t *testing.T) {
	resp := sdk.OnlineResponse{
		Users:  []string{"user1", "user2", "user3"},
		Cursor: "123",
	}

	if len(resp.Users) != 3 {
		t.Errorf("len(Users) = %d, want 3", len(resp.Users))
	}

	if resp.Cursor != "123" {
		t.Errorf("Cursor = %q, want %q", resp.Cursor, "123")
	}
}
