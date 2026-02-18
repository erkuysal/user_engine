//go:build integration

package test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/userengine/presence/pkg/presence"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

func TestPresence_InvisibleAndDevices(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 0})
	t.Cleanup(func() { _ = rdb.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available at %s: %v", addr, err)
	}

	scopeID := "it-" + uuid.New().String()
	userID := "user-" + uuid.New().String()[:8]

	svc := presence.NewService(rdb)
	svc.SetSessionTTL(30 * time.Second)
	if err := svc.LoadScripts(ctx); err != nil {
		t.Fatalf("LoadScripts: %v", err)
	}

	// Invisible connect should not appear online via LookupUsers.
	sessionInvisible := uuid.New().String()
	res, err := svc.Connect(ctx, presence.SessionMeta{
		ScopeID:     scopeID,
		UserID:      userID,
		SessionID:   sessionInvisible,
		DeviceType:  "desktop",
		Invisible:   true,
		ConnectedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Connect (invisible): %v", err)
	}
	if res.Version == 0 {
		t.Fatalf("expected non-zero version, got %d", res.Version)
	}

	statuses, err := svc.LookupUsers(ctx, scopeID, []string{userID})
	if err != nil {
		t.Fatalf("LookupUsers: %v", err)
	}
	if statuses[userID] != "offline" {
		t.Fatalf("LookupUsers invisible user = %q, want %q", statuses[userID], "offline")
	}

	adminStatuses, err := svc.LookupUsersWithInvisible(ctx, scopeID, []string{userID})
	if err != nil {
		t.Fatalf("LookupUsersWithInvisible: %v", err)
	}
	if adminStatuses[userID] != "invisible" {
		t.Fatalf("LookupUsersWithInvisible = %q, want %q", adminStatuses[userID], "invisible")
	}

	// Add a visible mobile session and ensure device tracking works.
	sessionMobile := uuid.New().String()
	if _, err := svc.Connect(ctx, presence.SessionMeta{
		ScopeID:     scopeID,
		UserID:      userID,
		SessionID:   sessionMobile,
		DeviceType:  "mobile",
		Invisible:   false,
		ConnectedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Connect (mobile): %v", err)
	}

	info, err := svc.GetUserDevices(ctx, scopeID, userID)
	if err != nil {
		t.Fatalf("GetUserDevices: %v", err)
	}
	if info.TotalSessions < 2 {
		t.Fatalf("TotalSessions=%d, want >=2", info.TotalSessions)
	}
	if info.Devices["desktop"] < 1 || info.Devices["mobile"] < 1 {
		t.Fatalf("Devices=%v, want desktop>=1 and mobile>=1", info.Devices)
	}
	if info.PrimaryDevice != "desktop" {
		t.Fatalf("PrimaryDevice=%q, want %q", info.PrimaryDevice, "desktop")
	}

	// Disconnect mobile session; desktop should remain.
	if _, err := svc.Disconnect(ctx, scopeID, sessionMobile, userID); err != nil {
		t.Fatalf("Disconnect (mobile): %v", err)
	}

	info2, err := svc.GetUserDevices(ctx, scopeID, userID)
	if err != nil {
		t.Fatalf("GetUserDevices after disconnect: %v", err)
	}
	if info2.Devices["mobile"] != 0 {
		t.Fatalf("mobile count=%d, want 0", info2.Devices["mobile"])
	}

	// Clean up all sessions.
	if _, err := svc.DisconnectUser(ctx, scopeID, userID); err != nil {
		t.Fatalf("DisconnectUser: %v", err)
	}
}

