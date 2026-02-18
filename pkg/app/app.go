package app

import (
	"context"
	"encoding/json"
	"errors"
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

	"github.com/go-redis/redis/v8"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// SetupLogging configures zerolog console logging (shared by all binaries).
func SetupLogging() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
}

// SignalContext returns a context that is cancelled on SIGINT/SIGTERM.
func SignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
}

// NewRedisClient creates a standalone Redis client from config.
// Note: UserEngine's core presence service currently expects *redis.Client.
func NewRedisClient(cfg *config.Config) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
}

// MustPingRedis verifies Redis connectivity or terminates.
func MustPingRedis(ctx context.Context, rdb *redis.Client) {
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal().Err(err).Msg("failed to connect to redis")
	}
}

// MustNewPresenceService initializes the presence service and loads Lua scripts.
func MustNewPresenceService(ctx context.Context, rdb *redis.Client, cfg *config.Config) *presence.Service {
	svc := presence.NewService(rdb)
	svc.SetSessionTTL(cfg.SessionTTL)
	svc.SetUseHashTags(cfg.RedisClusterEnabled)
	if err := svc.LoadScripts(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to load lua scripts")
	}
	return svc
}

// NewEventBus creates the Redis Pub/Sub event bus.
func NewEventBus(rdb *redis.Client) events.EventBus {
	return events.NewRedisPubSub(rdb)
}

// AddDiagnosticsEndpoints wires standard health + metrics endpoints onto mux.
func AddDiagnosticsEndpoints(mux *http.ServeMux, rdb *redis.Client, presenceSvc *presence.Service, version string) {
	checker := health.NewChecker(rdb, presenceSvc, version)

	mux.Handle("/health", checker.Handler())
	mux.Handle("/livez", health.LivenessHandler())
	mux.Handle("/readyz", health.ReadinessHandler(rdb))

	mux.Handle("/metrics", metrics.Global().Handler())
	mux.HandleFunc("/metrics/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(metrics.Global().Snapshot())
	})
}

// AddWorkerHealthEndpoint adds a lightweight /health endpoint (for workers).
func AddWorkerHealthEndpoint(mux *http.ServeMux) {
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

// ListenAndServeWithShutdown runs server and shuts it down when ctx is cancelled.
func ListenAndServeWithShutdown(ctx context.Context, server *http.Server, shutdownTimeout time.Duration) error {
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

