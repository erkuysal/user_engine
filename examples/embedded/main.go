// Example: Embedding presence directly into your Go application
//
// This example shows how to use the presence engine as a library,
// running all components within a single process.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/userengine/presence/pkg/events"
	"github.com/userengine/presence/pkg/presence"

	"github.com/go-redis/redis/v8"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Printf("Redis connection failed: %v\n", err)
		os.Exit(1)
	}

	// Initialize the presence service
	svc := presence.NewService(rdb)
	svc.SetSessionTTL(45 * time.Second)

	if err := svc.LoadScripts(ctx); err != nil {
		fmt.Printf("Failed to load Lua scripts: %v\n", err)
		os.Exit(1)
	}

	// Initialize event bus
	eventBus := events.NewRedisPubSub(rdb)

	// Start sweeper as a background goroutine
	sweeper := presence.NewSweeper(svc, eventBus, presence.SweeperConfig{
		Interval:       30 * time.Second,
		BudgetPerScope: 100 * time.Millisecond,
	})
	go sweeper.RunWorker(ctx)

	// Start debouncer as a background goroutine
	debouncer := presence.NewDebouncer(svc, eventBus, presence.DebouncerConfig{
		DisconnectDelay: 5 * time.Second,
		JobTTL:          60 * time.Second,
		PollInterval:    1 * time.Second,
	})
	go debouncer.RunWorker(ctx)

	// Subscribe to events for a scope
	eventCh, cancelSub, err := eventBus.Subscribe(ctx, "my-workspace")
	if err != nil {
		fmt.Printf("Failed to subscribe: %v\n", err)
		os.Exit(1)
	}
	defer cancelSub()

	// Handle events in the background
	go func() {
		for event := range eventCh {
			fmt.Printf("Event: %s - User %s (v%d)\n",
				event.Type, event.UserID, event.Version)
		}
	}()

	// Example: Connect a user
	result, err := svc.Connect(ctx, presence.SessionMeta{
		ScopeID:     "my-workspace",
		UserID:      "user-123",
		SessionID:   "session-abc",
		ConnectedAt: time.Now(),
	})
	if err != nil {
		fmt.Printf("Connect failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Connected: transition=%s, version=%d\n", result.Transition, result.Version)

	// Example: Check if user is online
	online, _ := svc.IsUserOnline(ctx, "my-workspace", "user-123")
	fmt.Printf("User online: %v\n", online)

	// Example: Lookup multiple users
	statuses, _ := svc.LookupUsers(ctx, "my-workspace", []string{"user-123", "user-456"})
	fmt.Printf("Statuses: %v\n", statuses)

	// Example: Heartbeat to keep session alive
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := svc.Heartbeat(ctx, "my-workspace", "session-abc"); err != nil {
					fmt.Printf("Heartbeat failed: %v\n", err)
				}
			}
		}
	}()

	// Optionally expose an HTTP endpoint
	http.HandleFunc("/api/presence/lookup", func(w http.ResponseWriter, r *http.Request) {
		// Your HTTP handler using svc.LookupUsers()
		w.Write([]byte("OK"))
	})

	go http.ListenAndServe(":8080", nil)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("Shutting down...")
	cancel()

	// Disconnect the user
	svc.Disconnect(ctx, "my-workspace", "session-abc", "user-123")
}

