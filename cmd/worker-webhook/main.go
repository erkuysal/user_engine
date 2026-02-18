package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/userengine/presence/pkg/app"
	"github.com/userengine/presence/pkg/config"
	"github.com/userengine/presence/pkg/webhooks"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

func main() {
	app.SetupLogging()

	// Load config
	cfg := config.Load()

	if !cfg.WebhookEnabled {
		log.Warn().Msg("webhooks are disabled (WEBHOOK_ENABLED=false), exiting")
		return
	}

	log.Info().
		Str("stream", cfg.WebhookStreamName).
		Str("group", cfg.WebhookConsumerGroup).
		Msg("starting webhook worker")

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

	// Generate unique consumer name
	consumerName := "webhook-worker-" + uuid.New().String()[:8]

	// Create and run worker
	worker := webhooks.NewWorker(rdb, cfg, consumerName)

	if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal().Err(err).Msg("webhook worker error")
	}
}
