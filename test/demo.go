//go:build ignore

// Manual test script - run with: go run test/demo.go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/userengine/presence/pkg/auth"
	"github.com/userengine/presence/pkg/events"
	"github.com/userengine/presence/pkg/presence"

	"github.com/go-redis/redis/v8"
)

func main() {
	ctx := context.Background()

	fmt.Println("=== UserEngine Manual Test ===\n")

	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Printf("❌ Redis connection failed: %v\n", err)
		return
	}
	fmt.Println("✅ Connected to Redis")

	// Initialize presence service
	svc := presence.NewService(rdb)
	svc.SetSessionTTL(30 * time.Second) // Shorter TTL for testing
	if err := svc.LoadScripts(ctx); err != nil {
		fmt.Printf("❌ Failed to load Lua scripts: %v\n", err)
		return
	}
	fmt.Println("✅ Presence service initialized")

	// Initialize event bus
	eventBus := events.NewRedisPubSub(rdb)

	// Subscribe to events
	eventCh, cancelSub, err := eventBus.Subscribe(ctx, "test-workspace")
	if err != nil {
		fmt.Printf("❌ Failed to subscribe: %v\n", err)
		return
	}
	defer cancelSub()
	fmt.Println("✅ Subscribed to events")

	// Listen for events in background
	go func() {
		for event := range eventCh {
			fmt.Printf("📢 Event: %s - User: %s (version: %d)\n",
				event.Type, event.UserID, event.Version)
		}
	}()

	fmt.Println("\n--- Test 1: Connect User ---")
	result, err := svc.Connect(ctx, presence.SessionMeta{
		ScopeID:     "test-workspace",
		UserID:      "alice",
		SessionID:   "session-1",
		ConnectedAt: time.Now(),
	})
	if err != nil {
		fmt.Printf("❌ Connect failed: %v\n", err)
		return
	}
	fmt.Printf("✅ Alice connected: transition=%s, version=%d\n", result.Transition, result.Version)

	// Publish online event manually (normally gateway does this)
	if result.Transition == "online" {
		eventBus.Publish(ctx, "test-workspace", events.PresenceEvent{
			EventID:    events.NewEventID(),
			ScopeID:    "test-workspace",
			Type:       events.EventTypeUserOnline,
			UserID:     "alice",
			Version:    result.Version,
			OccurredAt: time.Now(),
			Source:     "test",
		})
	}

	time.Sleep(100 * time.Millisecond) // Wait for event

	fmt.Println("\n--- Test 2: Check Online Status ---")
	online, _ := svc.IsUserOnline(ctx, "test-workspace", "alice")
	fmt.Printf("✅ Alice online: %v\n", online)

	online, _ = svc.IsUserOnline(ctx, "test-workspace", "bob")
	fmt.Printf("✅ Bob online: %v\n", online)

	fmt.Println("\n--- Test 3: Connect Second User ---")
	result2, _ := svc.Connect(ctx, presence.SessionMeta{
		ScopeID:     "test-workspace",
		UserID:      "bob",
		SessionID:   "session-2",
		ConnectedAt: time.Now(),
	})
	fmt.Printf("✅ Bob connected: transition=%s, version=%d\n", result2.Transition, result2.Version)

	if result2.Transition == "online" {
		eventBus.Publish(ctx, "test-workspace", events.PresenceEvent{
			EventID:    events.NewEventID(),
			ScopeID:    "test-workspace",
			Type:       events.EventTypeUserOnline,
			UserID:     "bob",
			Version:    result2.Version,
			OccurredAt: time.Now(),
			Source:     "test",
		})
	}

	time.Sleep(100 * time.Millisecond)

	fmt.Println("\n--- Test 4: Lookup Multiple Users ---")
	statuses, _ := svc.LookupUsers(ctx, "test-workspace", []string{"alice", "bob", "charlie"})
	for user, status := range statuses {
		fmt.Printf("  %s: %s\n", user, status)
	}

	fmt.Println("\n--- Test 5: Connect Second Session for Alice ---")
	result3, _ := svc.Connect(ctx, presence.SessionMeta{
		ScopeID:     "test-workspace",
		UserID:      "alice",
		SessionID:   "session-3",
		ConnectedAt: time.Now(),
	})
	fmt.Printf("✅ Alice's 2nd session: transition=%s (should be 'none')\n", result3.Transition)

	fmt.Println("\n--- Test 6: Heartbeat ---")
	err = svc.Heartbeat(ctx, "test-workspace", "session-1")
	if err != nil {
		fmt.Printf("❌ Heartbeat failed: %v\n", err)
	} else {
		fmt.Println("✅ Heartbeat successful")
	}

	fmt.Println("\n--- Test 7: Disconnect First Session ---")
	result4, _ := svc.Disconnect(ctx, "test-workspace", "session-1", "alice")
	fmt.Printf("✅ Disconnected session-1: transition=%s (should be 'none', alice has session-3)\n", result4.Transition)

	fmt.Println("\n--- Test 8: Disconnect Last Session ---")
	result5, _ := svc.Disconnect(ctx, "test-workspace", "session-3", "alice")
	fmt.Printf("✅ Disconnected session-3: transition=%s (should be 'offline')\n", result5.Transition)

	if result5.Transition == "offline" {
		eventBus.Publish(ctx, "test-workspace", events.PresenceEvent{
			EventID:    events.NewEventID(),
			ScopeID:    "test-workspace",
			Type:       events.EventTypeUserOffline,
			UserID:     "alice",
			Version:    result5.Version,
			OccurredAt: time.Now(),
			Source:     "test",
		})
	}

	time.Sleep(100 * time.Millisecond)

	fmt.Println("\n--- Test 9: Final Status Check ---")
	statuses, _ = svc.LookupUsers(ctx, "test-workspace", []string{"alice", "bob"})
	for user, status := range statuses {
		fmt.Printf("  %s: %s\n", user, status)
	}

	fmt.Println("\n--- Test 10: Generate JWT Token ---")
	validator := auth.NewValidator("dev-secret-change-in-production")
	token, _ := validator.GenerateToken("alice", []string{"test-workspace", "other-workspace"})
	fmt.Printf("✅ JWT Token (for API/WS testing):\n%s\n", token)

	// Cleanup
	svc.Disconnect(ctx, "test-workspace", "session-2", "bob")

	fmt.Println("\n=== All Tests Complete ===")
}

