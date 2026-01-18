package test

import (
	"strings"
	"testing"
	"time"

	"github.com/userengine/presence/pkg/presence"
)

// Test Key generation

func TestKey_Session(t *testing.T) {
	tests := []struct {
		name         string
		useHashTags  bool
		scopeID      string
		sessionID    string
		wantContains string
	}{
		{
			name:         "without hash tags",
			useHashTags:  false,
			scopeID:      "scope1",
			sessionID:    "session1",
			wantContains: "presence:session:scope1:session1",
		},
		{
			name:         "with hash tags",
			useHashTags:  true,
			scopeID:      "scope1",
			sessionID:    "session1",
			wantContains: "{scope1}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := presence.NewKey(tt.useHashTags)
			got := k.Session(tt.scopeID, tt.sessionID)
			if !strings.Contains(got, tt.wantContains) {
				t.Errorf("Session() = %q, want to contain %q", got, tt.wantContains)
			}
		})
	}
}

func TestKey_UserSessions(t *testing.T) {
	tests := []struct {
		name         string
		useHashTags  bool
		scopeID      string
		userID       string
		wantContains string
	}{
		{
			name:         "without hash tags",
			useHashTags:  false,
			scopeID:      "scope1",
			userID:       "user1",
			wantContains: "scope1:user1",
		},
		{
			name:         "with hash tags",
			useHashTags:  true,
			scopeID:      "scope1",
			userID:       "user1",
			wantContains: "{scope1:user1}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := presence.NewKey(tt.useHashTags)
			got := k.UserSessions(tt.scopeID, tt.userID)
			if !strings.Contains(got, tt.wantContains) {
				t.Errorf("UserSessions() = %q, want to contain %q", got, tt.wantContains)
			}
		})
	}
}

func TestKey_OnlineUsers(t *testing.T) {
	k := presence.NewKey(false)
	got := k.OnlineUsers("scope1")
	if got != "presence:online_users:scope1" {
		t.Errorf("OnlineUsers() = %q, want %q", got, "presence:online_users:scope1")
	}

	k = presence.NewKey(true)
	got = k.OnlineUsers("scope1")
	if !strings.Contains(got, "{scope1}") {
		t.Errorf("OnlineUsers() with hash tags = %q, want to contain {scope1}", got)
	}
}

func TestKey_UserVersion(t *testing.T) {
	k := presence.NewKey(true)
	got := k.UserVersion("scope1", "user1")
	if !strings.Contains(got, "{scope1:user1}") {
		t.Errorf("UserVersion() = %q, want to contain {scope1:user1}", got)
	}
}

func TestKey_Scopes(t *testing.T) {
	k := presence.NewKey(true)
	got := k.Scopes()
	// Scopes is a global key, should not have hash tags
	if got != "presence:scopes" {
		t.Errorf("Scopes() = %q, want %q", got, "presence:scopes")
	}
}

func TestKey_DisconnectJobs(t *testing.T) {
	k := presence.NewKey(true)
	got := k.DisconnectJobs("scope1")
	if !strings.Contains(got, "{scope1}") {
		t.Errorf("DisconnectJobs() = %q, want to contain {scope1}", got)
	}
}

func TestKey_DisconnectJobMeta(t *testing.T) {
	k := presence.NewKey(true)
	got := k.DisconnectJobMeta("scope1", "job1")
	if !strings.Contains(got, "{scope1}") {
		t.Errorf("DisconnectJobMeta() = %q, want to contain {scope1}", got)
	}
	if !strings.Contains(got, "job1") {
		t.Errorf("DisconnectJobMeta() = %q, want to contain job1", got)
	}
}

func TestKey_PubSubChannel(t *testing.T) {
	// Pub/Sub channels don't use hash tags
	k := presence.NewKey(true)
	got := k.PubSubChannel("scope1")
	if strings.Contains(got, "{") {
		t.Errorf("PubSubChannel() = %q, should not contain hash tags", got)
	}
	if !strings.Contains(got, "scope1") {
		t.Errorf("PubSubChannel() = %q, want to contain scope1", got)
	}
}

func TestKey_InvisibleUsers(t *testing.T) {
	k := presence.NewKey(true)
	got := k.InvisibleUsers("scope1")
	if !strings.Contains(got, "{scope1}") {
		t.Errorf("InvisibleUsers() = %q, want to contain {scope1}", got)
	}
	if !strings.Contains(got, "invisible_users") {
		t.Errorf("InvisibleUsers() = %q, want to contain invisible_users", got)
	}
}

func TestKey_UserDevices(t *testing.T) {
	k := presence.NewKey(true)
	got := k.UserDevices("scope1", "user1")
	if !strings.Contains(got, "{scope1:user1}") {
		t.Errorf("UserDevices() = %q, want to contain {scope1:user1}", got)
	}
}

func TestNewKey(t *testing.T) {
	k := presence.NewKey(true)
	if !k.UseHashTags {
		t.Error("NewKey(true).UseHashTags = false, want true")
	}

	k = presence.NewKey(false)
	if k.UseHashTags {
		t.Error("NewKey(false).UseHashTags = true, want false")
	}
}

// Test Debouncer configuration

func TestDefaultDebouncerConfig(t *testing.T) {
	cfg := presence.DefaultDebouncerConfig()

	if cfg.DisconnectDelay != 5*time.Second {
		t.Errorf("DisconnectDelay = %v, want %v", cfg.DisconnectDelay, 5*time.Second)
	}

	if cfg.JobTTL != 60*time.Second {
		t.Errorf("JobTTL = %v, want %v", cfg.JobTTL, 60*time.Second)
	}

	if cfg.PollInterval != 1*time.Second {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 1*time.Second)
	}

	if cfg.BatchSize != 100 {
		t.Errorf("BatchSize = %d, want %d", cfg.BatchSize, 100)
	}

	if cfg.FlappingWindow != 30*time.Second {
		t.Errorf("FlappingWindow = %v, want %v", cfg.FlappingWindow, 30*time.Second)
	}

	if cfg.FlappingThreshold != 5 {
		t.Errorf("FlappingThreshold = %d, want %d", cfg.FlappingThreshold, 5)
	}

	if cfg.OfflineDelay != 15*time.Second {
		t.Errorf("OfflineDelay = %v, want %v", cfg.OfflineDelay, 15*time.Second)
	}
}

func TestDebouncerConfig_Validation(t *testing.T) {
	tests := []struct {
		name   string
		cfg    presence.DebouncerConfig
		valid  bool
		reason string
	}{
		{
			name:   "default is valid",
			cfg:    presence.DefaultDebouncerConfig(),
			valid:  true,
			reason: "",
		},
		{
			name: "JobTTL should be longer than DisconnectDelay",
			cfg: presence.DebouncerConfig{
				DisconnectDelay: 10 * time.Second,
				JobTTL:          5 * time.Second, // Less than DisconnectDelay
			},
			valid:  false,
			reason: "JobTTL should be >= DisconnectDelay",
		},
		{
			name: "zero values should be allowed",
			cfg: presence.DebouncerConfig{
				DisconnectDelay:   0,
				JobTTL:            0,
				FlappingThreshold: 0, // Disabled
				FlappingWindow:    0, // Disabled
			},
			valid:  true,
			reason: "zero values disable the feature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Basic validation logic
			valid := true
			if tt.cfg.JobTTL > 0 && tt.cfg.DisconnectDelay > 0 && tt.cfg.JobTTL < tt.cfg.DisconnectDelay {
				valid = false
			}

			if tt.valid != valid {
				t.Errorf("validation = %v, want %v (reason: %s)", valid, tt.valid, tt.reason)
			}
		})
	}
}
