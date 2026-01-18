// Example: Using the SDK to connect to presence services remotely
//
// This example shows how to use the SDK client when presence
// is running as separate services.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/userengine/presence/pkg/auth"
	"github.com/userengine/presence/pkg/events"
	"github.com/userengine/presence/pkg/sdk"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Generate a token (in production, your auth service would provide this)
	validator := auth.NewValidator("dev-secret-change-in-production")
	token, err := validator.GenerateToken("user-123", []string{"workspace-1"})
	if err != nil {
		fmt.Printf("Failed to generate token: %v\n", err)
		os.Exit(1)
	}

	// Create the SDK client
	client := sdk.NewClient(
		"http://localhost:8081", // API service
		"http://localhost:8080", // Gateway service (for WebSocket)
		token,
	)

	// Example: Lookup presence for specific users
	statuses, err := client.Lookup(ctx, "workspace-1", []string{"user-1", "user-2", "user-3"})
	if err != nil {
		fmt.Printf("Lookup failed: %v\n", err)
	} else {
		fmt.Printf("User statuses: %v\n", statuses)
	}

	// Example: Check if a single user is online
	online, err := client.IsOnline(ctx, "workspace-1", "user-1")
	if err != nil {
		fmt.Printf("IsOnline failed: %v\n", err)
	} else {
		fmt.Printf("User-1 online: %v\n", online)
	}

	// Example: Get all online users (with pagination handled internally)
	allOnline, err := client.GetAllOnline(ctx, "workspace-1")
	if err != nil {
		fmt.Printf("GetAllOnline failed: %v\n", err)
	} else {
		fmt.Printf("All online users: %v\n", allOnline)
	}

	// Example: Connect to WebSocket for real-time events
	conn, err := client.Connect(ctx, "workspace-1")
	if err != nil {
		fmt.Printf("WebSocket connect failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Set up event handlers
	conn.OnEvent = func(event events.PresenceEvent) {
		fmt.Printf("Real-time event: %s - User %s (v%d)\n",
			event.Type, event.UserID, event.Version)
	}

	conn.OnReconnectRequired = func(reason string) {
		fmt.Printf("Reconnect required: %s\n", reason)
		// In production: reconnect and re-fetch snapshot
	}

	conn.OnError = func(err error) {
		fmt.Printf("WebSocket error: %v\n", err)
	}

	// Or use the channel-based API
	go func() {
		for event := range conn.Events() {
			fmt.Printf("Event from channel: %s - %s\n", event.Type, event.UserID)
		}
	}()

	fmt.Println("Connected to presence service. Listening for events...")
	fmt.Println("Press Ctrl+C to exit")

	// Wait for shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("Shutting down...")
}
