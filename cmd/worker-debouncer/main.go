package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/userengine/presence/pkg/app"
	"github.com/userengine/presence/pkg/config"
	"github.com/userengine/presence/pkg/events"
	"github.com/userengine/presence/pkg/presence"

	"github.com/rs/zerolog/log"
)

func main() {
	app.SetupLogging()

	// Load config
	cfg := config.Load()
	log.Info().Msg("starting debouncer worker")

	ctx, stop := app.SignalContext(context.Background())
	defer stop()

	// Start health check server
	go func() {
		mux := http.NewServeMux()
		app.AddWorkerHealthEndpoint(mux)
		server := &http.Server{
			Addr:              cfg.WorkerHealthAddr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		}
		log.Info().Str("addr", cfg.WorkerHealthAddr).Msg("starting worker health server")
		if err := app.ListenAndServeWithShutdown(ctx, server, 5*time.Second); err != nil {
			log.Error().Err(err).Msg("worker health server failed")
		}
	}()

	// Connect to Redis
	rdb := app.NewRedisClient(cfg)
	defer rdb.Close()

	app.MustPingRedis(ctx, rdb)

	// Initialize presence service
	presenceSvc := app.MustNewPresenceService(ctx, rdb, cfg)

	// Initialize event bus
	eventBus := events.NewRedisPubSub(rdb)

	// Initialize debouncer with config
	debouncer := presence.NewDebouncer(presenceSvc, eventBus, presence.DebouncerConfig{
		DisconnectDelay: cfg.DisconnectDebounceDelay,
		JobTTL:          cfg.DebounceJobTTL,
		PollInterval:    cfg.DebouncerPollInterval,
		BatchSize:       100,
	})

	// Run debouncer as a worker (blocks until cancelled)
	if err := debouncer.RunWorker(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal().Err(err).Msg("debouncer worker error")
	}
}
