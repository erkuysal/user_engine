package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/userengine/presence/pkg/config"
	"github.com/userengine/presence/pkg/events"
	"github.com/userengine/presence/pkg/presence"

	"net/http"

	"github.com/go-redis/redis/v8"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Load config
	cfg := config.Load()
	log.Info().Msg("starting debouncer worker")

	// Start health check server
	go func() {
		http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		})
		log.Info().Msg("starting health check server on :8080")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Error().Err(err).Msg("health check server failed")
		}
	}()

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
	if err := presenceSvc.LoadScripts(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to load lua scripts")
	}

	// Initialize event bus
	eventBus := events.NewRedisPubSub(rdb)

	// Initialize debouncer with config
	debouncer := presence.NewDebouncer(presenceSvc, eventBus, presence.DebouncerConfig{
		DisconnectDelay: cfg.DisconnectDebounceDelay,
		JobTTL:          cfg.DebounceJobTTL,
		PollInterval:    cfg.DebouncerPollInterval,
		BatchSize:       100,
	})

	// Setup shutdown signal
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Info().Msg("shutting down debouncer")
		cancel()
	}()

	// Run debouncer as a worker (blocks until cancelled)
	debouncer.RunWorker(ctx)
}
