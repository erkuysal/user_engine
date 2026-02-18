package main

import (
	"context"
	"net/http"
	"time"

	"github.com/userengine/presence/pkg/app"
	"github.com/userengine/presence/pkg/config"
	"github.com/userengine/presence/pkg/events"
	"github.com/userengine/presence/pkg/ws"

	"github.com/rs/zerolog/log"
)

const version = "1.0.0"

func main() {
	app.SetupLogging()

	// Load and validate config
	cfg := config.Load()

	// Show environment banner
	log.Info().Msgf("🚀 Gateway starting in [%s] mode", cfg.Environment)
	log.Info().Str("addr", cfg.GatewayAddr).Str("env", cfg.Environment).Msg("starting gateway")

	ctx, stop := app.SignalContext(context.Background())
	defer stop()

	// Connect to Redis
	rdb := app.NewRedisClient(cfg)
	defer rdb.Close()

	app.MustPingRedis(ctx, rdb)

	// Initialize presence service
	presenceSvc := app.MustNewPresenceService(ctx, rdb, cfg)

	// Initialize event bus
	eventBus := events.NewRedisPubSub(rdb)

	// Initialize WebSocket hub
	hub := ws.NewHub(presenceSvc, eventBus, cfg)
	go hub.Run(ctx)

	// Setup HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.HandleWebSocket)
	app.AddDiagnosticsEndpoints(mux, rdb, presenceSvc, version)

	server := &http.Server{
		Addr:         cfg.GatewayAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		log.Info().Msg("shutting down gateway")
		hub.Shutdown()
	}()

	log.Info().Str("addr", cfg.GatewayAddr).Msg("gateway listening")
	if err := app.ListenAndServeWithShutdown(ctx, server, 30*time.Second); err != nil {
		log.Fatal().Err(err).Msg("server error")
	}
}
