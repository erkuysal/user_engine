package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/userengine/presence/pkg/auth"
	"github.com/userengine/presence/pkg/config"
	"github.com/userengine/presence/pkg/events"
	"github.com/userengine/presence/pkg/health"
	"github.com/userengine/presence/pkg/metrics"
	"github.com/userengine/presence/pkg/presence"

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
	log.Info().Msgf("🚀 API starting in [%s] mode", cfg.Environment)
	log.Info().Str("addr", cfg.APIAddr).Str("env", cfg.Environment).Msg("starting api")

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
	presenceSvc.SetUseHashTags(cfg.RedisClusterEnabled) // Enable hash tags for cluster mode
	if err := presenceSvc.LoadScripts(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to load lua scripts")
	}

	// Initialize event bus (for server-side disconnect events)
	eventBus := events.NewRedisPubSub(rdb)

	// Initialize auth validator
	authValidator := auth.NewValidator(cfg.JWTSecret)

	// Setup HTTP server
	mux := http.NewServeMux()

	// GET /presence/online - Admin/debug endpoint
	mux.HandleFunc("GET /presence/online", func(w http.ResponseWriter, r *http.Request) {
		handleGetOnline(w, r, presenceSvc, authValidator)
	})

	// POST /presence/lookup - Primary UI endpoint
	mux.HandleFunc("POST /presence/lookup", func(w http.ResponseWriter, r *http.Request) {
		handleLookup(w, r, presenceSvc, authValidator)
	})

	// POST /presence/tabs - Session counts per user
	mux.HandleFunc("POST /presence/tabs", func(w http.ResponseWriter, r *http.Request) {
		handleTabs(w, r, presenceSvc, authValidator)
	})

	// POST /presence/disconnect_user - Server-triggered logout
	mux.HandleFunc("POST /presence/disconnect_user", func(w http.ResponseWriter, r *http.Request) {
		handleDisconnectUser(w, r, presenceSvc, authValidator, eventBus)
	})

	// POST /presence/devices - Device presence per user
	mux.HandleFunc("POST /presence/devices", func(w http.ResponseWriter, r *http.Request) {
		handleDevices(w, r, presenceSvc, authValidator)
	})

	// Initialize health checker
	healthChecker := health.NewChecker(rdb, presenceSvc, version)

	// Health check endpoints
	mux.Handle("/health", healthChecker.Handler())
	mux.Handle("/livez", health.LivenessHandler())
	mux.Handle("/readyz", health.ReadinessHandler(rdb))

	// Prometheus metrics endpoint
	mux.Handle("/metrics", metrics.Global().Handler())

	// JSON metrics endpoint for debugging
	mux.HandleFunc("/metrics/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metrics.Global().Snapshot())
	})

	server := &http.Server{
		Addr:         cfg.APIAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Info().Msg("shutting down api")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	log.Info().Str("addr", cfg.APIAddr).Msg("api listening")
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("server error")
	}
}

func handleGetOnline(w http.ResponseWriter, r *http.Request, svc *presence.Service, authValidator *auth.Validator) {
	// Extract and validate JWT
	claims, err := authValidator.ValidateRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	scopeID := r.URL.Query().Get("scope_id")
	if scopeID == "" {
		http.Error(w, "scope_id required", http.StatusBadRequest)
		return
	}

	// Verify scope access
	if !claims.HasScopeAccess(scopeID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	cursor := r.URL.Query().Get("cursor")
	if cursor == "" {
		cursor = "0"
	}

	users, nextCursor, err := svc.GetOnlineUsers(r.Context(), scopeID, cursor, 100)
	if err != nil {
		log.Error().Err(err).Str("scope_id", scopeID).Msg("failed to get online users")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"users":  users,
		"cursor": nextCursor,
	})
}

type lookupRequest struct {
	ScopeID string   `json:"scope_id"`
	UserIDs []string `json:"user_ids"`
}

func handleLookup(w http.ResponseWriter, r *http.Request, svc *presence.Service, authValidator *auth.Validator) {
	// Extract and validate JWT
	claims, err := authValidator.ValidateRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req lookupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req.ScopeID == "" {
		http.Error(w, "scope_id required", http.StatusBadRequest)
		return
	}

	if len(req.UserIDs) == 0 {
		http.Error(w, "user_ids required", http.StatusBadRequest)
		return
	}

	if len(req.UserIDs) > 1000 {
		http.Error(w, "too many user_ids (max 1000)", http.StatusBadRequest)
		return
	}

	// Verify scope access
	if !claims.HasScopeAccess(req.ScopeID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	statuses, err := svc.LookupUsers(r.Context(), req.ScopeID, req.UserIDs)
	if err != nil {
		log.Error().Err(err).Str("scope_id", req.ScopeID).Msg("failed to lookup users")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"statuses": statuses,
	})
}

type tabsRequest struct {
	ScopeID string   `json:"scope_id"`
	UserIDs []string `json:"user_ids"`
}

func handleTabs(w http.ResponseWriter, r *http.Request, svc *presence.Service, authValidator *auth.Validator) {
	claims, err := authValidator.ValidateRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req tabsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req.ScopeID == "" {
		http.Error(w, "scope_id required", http.StatusBadRequest)
		return
	}

	if len(req.UserIDs) == 0 {
		http.Error(w, "user_ids required", http.StatusBadRequest)
		return
	}

	if len(req.UserIDs) > 1000 {
		http.Error(w, "too many user_ids (max 1000)", http.StatusBadRequest)
		return
	}

	if !claims.HasScopeAccess(req.ScopeID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	counts, err := svc.GetUsersSessionCounts(r.Context(), req.ScopeID, req.UserIDs)
	if err != nil {
		log.Error().Err(err).Str("scope_id", req.ScopeID).Msg("failed to get tab counts")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"counts": counts,
	})
}

type disconnectUserRequest struct {
	ScopeID string `json:"scope_id"`
	UserID  string `json:"user_id"`
}

func handleDisconnectUser(w http.ResponseWriter, r *http.Request, svc *presence.Service, authValidator *auth.Validator, eventBus events.Publisher) {
	claims, err := authValidator.ValidateRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req disconnectUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req.ScopeID == "" || req.UserID == "" {
		http.Error(w, "scope_id and user_id required", http.StatusBadRequest)
		return
	}

	if !claims.HasScopeAccess(req.ScopeID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	result, err := svc.DisconnectUser(r.Context(), req.ScopeID, req.UserID)
	if err != nil {
		log.Error().Err(err).Str("scope_id", req.ScopeID).Str("user_id", req.UserID).Msg("failed to disconnect user")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if result.Transition == "offline" {
		event := events.PresenceEvent{
			EventID:    events.NewEventID(),
			ScopeID:    req.ScopeID,
			Type:       events.EventTypeUserOffline,
			UserID:     req.UserID,
			Version:    result.Version,
			TabCount:   result.SessionCount,
			OccurredAt: time.Now().UTC(),
			Source:     "api",
		}

		if err := eventBus.Publish(r.Context(), req.ScopeID, event); err != nil {
			log.Error().Err(err).Str("scope_id", req.ScopeID).Str("user_id", req.UserID).Msg("failed to publish offline event")
		}
	} else if result.SessionCount > 0 {
		event := events.PresenceEvent{
			EventID:    events.NewEventID(),
			ScopeID:    req.ScopeID,
			Type:       events.EventTypeUserTabs,
			UserID:     req.UserID,
			Version:    result.Version,
			TabCount:   result.SessionCount,
			OccurredAt: time.Now().UTC(),
			Source:     "api",
		}

		if err := eventBus.Publish(r.Context(), req.ScopeID, event); err != nil {
			log.Error().Err(err).Str("scope_id", req.ScopeID).Str("user_id", req.UserID).Msg("failed to publish tab count event")
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"transition":    result.Transition,
		"version":       result.Version,
		"session_count": result.SessionCount,
	})
}

type devicesRequest struct {
	ScopeID string   `json:"scope_id"`
	UserIDs []string `json:"user_ids"`
}

func handleDevices(w http.ResponseWriter, r *http.Request, svc *presence.Service, authValidator *auth.Validator) {
	claims, err := authValidator.ValidateRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req devicesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req.ScopeID == "" {
		http.Error(w, "scope_id required", http.StatusBadRequest)
		return
	}

	if len(req.UserIDs) == 0 {
		http.Error(w, "user_ids required", http.StatusBadRequest)
		return
	}

	if len(req.UserIDs) > 1000 {
		http.Error(w, "too many user_ids (max 1000)", http.StatusBadRequest)
		return
	}

	if !claims.HasScopeAccess(req.ScopeID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	devices, err := svc.GetUsersDevices(r.Context(), req.ScopeID, req.UserIDs)
	if err != nil {
		log.Error().Err(err).Str("scope_id", req.ScopeID).Msg("failed to get user devices")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"devices": devices,
	})
}
