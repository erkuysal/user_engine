package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/userengine/presence/pkg/config"
	"github.com/userengine/presence/pkg/events"
	"github.com/userengine/presence/pkg/health"
	"github.com/userengine/presence/pkg/metrics"
	"github.com/userengine/presence/pkg/presence"
	"github.com/userengine/presence/pkg/ws"

	"github.com/go-redis/redis/v8"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const version = "1.0.0"

func main() {
	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Load and validate config
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal().Err(err).Msg("configuration validation failed")
	}

	// Show environment banner
	log.Info().Msgf("🚀 Gateway starting in [%s] mode", cfg.Environment)
	log.Info().Str("addr", cfg.GatewayAddr).Str("env", cfg.Environment).Msg("starting gateway")

	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer rdb.Close()

	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal().Err(err).Msg("failed to connect to redis")
	}

	// Initialize presence service
	presenceSvc := presence.NewService(rdb)
	presenceSvc.SetSessionTTL(cfg.SessionTTL)
	presenceSvc.SetUseHashTags(cfg.RedisClusterEnabled) // Enable hash tags for cluster mode
	if err := presenceSvc.LoadScripts(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to load lua scripts")
	}

	// Initialize event bus
	eventBus := events.NewRedisPubSub(rdb)

	// Initialize WebSocket hub
	hub := ws.NewHub(presenceSvc, eventBus, cfg)
	go hub.Run(ctx)

	// Initialize health checker
	healthChecker := health.NewChecker(rdb, presenceSvc, version)

	// Setup HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.HandleWebSocket)

	// Health check endpoints
	mux.Handle("/health", healthChecker.Handler())      // Deep health check
	mux.Handle("/livez", health.LivenessHandler())      // Simple liveness probe
	mux.Handle("/readyz", health.ReadinessHandler(rdb)) // Readiness probe

	// Prometheus metrics endpoint
	mux.Handle("/metrics", metrics.Global().Handler())

	// JSON metrics endpoint for debugging
	mux.HandleFunc("/metrics/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metrics.Global().Snapshot())
	})

	server := &http.Server{
		Addr:         cfg.GatewayAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Info().Msg("shutting down gateway")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		hub.Shutdown()
		server.Shutdown(shutdownCtx)
	}()

	log.Info().Str("addr", cfg.GatewayAddr).Msg("gateway listening")
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("server error")
	}
}
