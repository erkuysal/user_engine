//go:build ignore

// Interactive demo - keeps a user online with heartbeats
// Run with: go run test/interactive.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/userengine/presence/pkg/auth"
	"github.com/userengine/presence/pkg/events"
	"github.com/userengine/presence/pkg/presence"

	"github.com/go-redis/redis/v8"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Printf("Redis connection failed: %v\n", err)
		return
	}

	// Initialize
	svc := presence.NewService(rdb)
	svc.SetSessionTTL(30 * time.Second)
	svc.LoadScripts(ctx)

	eventBus := events.NewRedisPubSub(rdb)

	// Generate token
	validator := auth.NewValidator("dev-secret-change-in-production")
	token, _ := validator.GenerateToken("demo-user", []string{"test-workspace"})

	fmt.Println("=== Interactive Presence Demo ===")
	fmt.Println()
	fmt.Printf("JWT Token (use for API/WebSocket):\n%s\n", token)
	fmt.Println()

	// Subscribe to events
	eventCh, cancelSub, _ := eventBus.Subscribe(ctx, "test-workspace")
	defer cancelSub()

	go func() {
		for event := range eventCh {
			fmt.Printf("📢 [%s] %s: %s (v%d)\n",
				event.Source, event.Type, event.UserID, event.Version)
		}
	}()

	// Connect user
	sessionID := fmt.Sprintf("session-%d", time.Now().UnixNano())
	result, _ := svc.Connect(ctx, presence.SessionMeta{
		ScopeID:     "test-workspace",
		UserID:      "demo-user",
		SessionID:   sessionID,
		ConnectedAt: time.Now(),
	})

	if result.Transition == "online" {
		eventBus.Publish(ctx, "test-workspace", events.PresenceEvent{
			EventID:    events.NewEventID(),
			ScopeID:    "test-workspace",
			Type:       events.EventTypeUserOnline,
			UserID:     "demo-user",
			Version:    result.Version,
			OccurredAt: time.Now(),
			Source:     "demo",
		})
	}

	fmt.Println("✅ demo-user is now ONLINE")
	fmt.Println()
	fmt.Println("Test with these commands in another terminal:")
	fmt.Println()
	fmt.Println("PowerShell - Lookup users:")
	fmt.Printf(`$headers = @{ "Authorization" = "Bearer %s"; "Content-Type" = "application/json" }; Invoke-RestMethod -Uri "http://localhost:8081/presence/lookup" -Method POST -Headers $headers -Body '{"scope_id": "test-workspace", "user_ids": ["demo-user"]}' | ConvertTo-Json`+"\n", token)
	fmt.Println()
	fmt.Println("WebSocket URL:")
	fmt.Printf("ws://localhost:8080/ws?scope_id=test-workspace&token=%s\n", token)
	fmt.Println()
	fmt.Println("Press Ctrl+C to disconnect and exit...")

	// Heartbeat loop
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := svc.Heartbeat(ctx, "test-workspace", sessionID); err != nil {
					fmt.Printf("❌ Heartbeat failed: %v\n", err)
				} else {
					fmt.Println("💓 Heartbeat sent")
				}
			}
		}
	}()

	// Wait for Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nDisconnecting...")
	result2, _ := svc.Disconnect(ctx, "test-workspace", sessionID, "demo-user")
	if result2.Transition == "offline" {
		eventBus.Publish(ctx, "test-workspace", events.PresenceEvent{
			EventID:    events.NewEventID(),
			ScopeID:    "test-workspace",
			Type:       events.EventTypeUserOffline,
			UserID:     "demo-user",
			Version:    result2.Version,
			OccurredAt: time.Now(),
			Source:     "demo",
		})
	}
	fmt.Println("👋 demo-user is now OFFLINE")
}
