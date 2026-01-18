package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/userengine/presence/pkg/config"
	"github.com/userengine/presence/pkg/webhooks"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

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

	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer rdb.Close()

	ctx, cancel := context.WithCancel(context.Background())

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal().Err(err).Msg("failed to connect to redis")
	}

	// Generate unique consumer name
	consumerName := "webhook-worker-" + uuid.New().String()[:8]

	// Create and run worker
	worker := webhooks.NewWorker(rdb, cfg, consumerName)

	// Handle shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Info().Msg("shutting down webhook worker")
		cancel()
	}()

	if err := worker.Run(ctx); err != nil && err != context.Canceled {
		log.Fatal().Err(err).Msg("webhook worker error")
	}
}
