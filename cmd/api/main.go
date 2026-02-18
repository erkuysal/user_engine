package main

import (
	"context"
	"net/http"
	"time"

	"github.com/userengine/presence/pkg/app"
	"github.com/userengine/presence/pkg/auth"
	"github.com/userengine/presence/pkg/config"
	"github.com/userengine/presence/pkg/events"
	"github.com/userengine/presence/pkg/httpx"
	"github.com/userengine/presence/pkg/presence"

	"github.com/rs/zerolog/log"
)

const version = "1.0.0"

func main() {
	app.SetupLogging()

	// Load and validate config
	cfg := config.Load()

	// Show environment banner
	log.Info().Msgf("🚀 API starting in [%s] mode", cfg.Environment)
	log.Info().Str("addr", cfg.APIAddr).Str("env", cfg.Environment).Msg("starting api")

	ctx, stop := app.SignalContext(context.Background())
	defer stop()

	// Connect to Redis
	rdb := app.NewRedisClient(cfg)
	defer rdb.Close()

	app.MustPingRedis(ctx, rdb)

	// Initialize presence service
	presenceSvc := app.MustNewPresenceService(ctx, rdb, cfg)

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

	// GET /presence/count - O(1) online user count
	mux.HandleFunc("GET /presence/count", func(w http.ResponseWriter, r *http.Request) {
		handleCount(w, r, presenceSvc, authValidator)
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

	// Standard health + metrics endpoints
	app.AddDiagnosticsEndpoints(mux, rdb, presenceSvc, version)

	server := &http.Server{
		Addr:         cfg.APIAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		log.Info().Msg("shutting down api")
	}()

	log.Info().Str("addr", cfg.APIAddr).Msg("api listening")
	if err := app.ListenAndServeWithShutdown(ctx, server, 10*time.Second); err != nil {
		log.Fatal().Err(err).Msg("server error")
	}
}

func handleGetOnline(w http.ResponseWriter, r *http.Request, svc *presence.Service, authValidator *auth.Validator) {
	// Extract and validate JWT
	claims, ok := requireAuth(w, r, authValidator)
	if !ok {
		return
	}

	scopeID := r.URL.Query().Get("scope_id")
	if scopeID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "scope_id required")
		return
	}

	// Verify scope access
	if !requireScopeAccess(w, claims, scopeID) {
		return
	}

	cursor := r.URL.Query().Get("cursor")
	if cursor == "" {
		cursor = "0"
	}

	users, nextCursor, err := svc.GetOnlineUsers(r.Context(), scopeID, cursor, 100)
	if err != nil {
		log.Error().Err(err).Str("scope_id", scopeID).Msg("failed to get online users")
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"users":  users,
		"cursor": nextCursor,
	})
}

func handleCount(w http.ResponseWriter, r *http.Request, svc *presence.Service, authValidator *auth.Validator) {
	// Extract and validate JWT
	claims, ok := requireAuth(w, r, authValidator)
	if !ok {
		return
	}

	scopeID := r.URL.Query().Get("scope_id")
	if scopeID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "scope_id required")
		return
	}

	// Verify scope access
	if !requireScopeAccess(w, claims, scopeID) {
		return
	}

	count, err := svc.GetOnlineUsersCount(r.Context(), scopeID)
	if err != nil {
		log.Error().Err(err).Str("scope_id", scopeID).Msg("failed to get online users count")
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"count": count,
	})
}

type lookupRequest struct {
	ScopeID string   `json:"scope_id"`
	UserIDs []string `json:"user_ids"`
}

func handleLookup(w http.ResponseWriter, r *http.Request, svc *presence.Service, authValidator *auth.Validator) {
	// Extract and validate JWT
	claims, ok := requireAuth(w, r, authValidator)
	if !ok {
		return
	}

	var req lookupRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if req.ScopeID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "scope_id required")
		return
	}

	if len(req.UserIDs) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "user_ids required")
		return
	}

	if len(req.UserIDs) > 1000 {
		httpx.WriteError(w, http.StatusBadRequest, "too many user_ids (max 1000)")
		return
	}

	// Verify scope access
	if !requireScopeAccess(w, claims, req.ScopeID) {
		return
	}

	statuses, err := svc.LookupUsers(r.Context(), req.ScopeID, req.UserIDs)
	if err != nil {
		log.Error().Err(err).Str("scope_id", req.ScopeID).Msg("failed to lookup users")
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"statuses": statuses,
	})
}

type tabsRequest struct {
	ScopeID string   `json:"scope_id"`
	UserIDs []string `json:"user_ids"`
}

func handleTabs(w http.ResponseWriter, r *http.Request, svc *presence.Service, authValidator *auth.Validator) {
	claims, ok := requireAuth(w, r, authValidator)
	if !ok {
		return
	}

	var req tabsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if req.ScopeID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "scope_id required")
		return
	}

	if len(req.UserIDs) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "user_ids required")
		return
	}

	if len(req.UserIDs) > 1000 {
		httpx.WriteError(w, http.StatusBadRequest, "too many user_ids (max 1000)")
		return
	}

	if !requireScopeAccess(w, claims, req.ScopeID) {
		return
	}

	counts, err := svc.GetUsersSessionCounts(r.Context(), req.ScopeID, req.UserIDs)
	if err != nil {
		log.Error().Err(err).Str("scope_id", req.ScopeID).Msg("failed to get tab counts")
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"counts": counts,
	})
}

type disconnectUserRequest struct {
	ScopeID string `json:"scope_id"`
	UserID  string `json:"user_id"`
}

func handleDisconnectUser(w http.ResponseWriter, r *http.Request, svc *presence.Service, authValidator *auth.Validator, eventBus events.Publisher) {
	claims, ok := requireAuth(w, r, authValidator)
	if !ok {
		return
	}

	var req disconnectUserRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if req.ScopeID == "" || req.UserID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "scope_id and user_id required")
		return
	}

	if !requireScopeAccess(w, claims, req.ScopeID) {
		return
	}

	result, err := svc.DisconnectUser(r.Context(), req.ScopeID, req.UserID)
	if err != nil {
		log.Error().Err(err).Str("scope_id", req.ScopeID).Str("user_id", req.UserID).Msg("failed to disconnect user")
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
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

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
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
	claims, ok := requireAuth(w, r, authValidator)
	if !ok {
		return
	}

	var req devicesRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if req.ScopeID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "scope_id required")
		return
	}

	if len(req.UserIDs) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "user_ids required")
		return
	}

	if len(req.UserIDs) > 1000 {
		httpx.WriteError(w, http.StatusBadRequest, "too many user_ids (max 1000)")
		return
	}

	if !requireScopeAccess(w, claims, req.ScopeID) {
		return
	}

	devices, err := svc.GetUsersDevices(r.Context(), req.ScopeID, req.UserIDs)
	if err != nil {
		log.Error().Err(err).Str("scope_id", req.ScopeID).Msg("failed to get user devices")
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"devices": devices,
	})
}

func requireAuth(w http.ResponseWriter, r *http.Request, v *auth.Validator) (*auth.Claims, bool) {
	claims, err := v.ValidateRequest(r)
	if err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	return claims, true
}

func requireScopeAccess(w http.ResponseWriter, claims *auth.Claims, scopeID string) bool {
	if !claims.HasScopeAccess(scopeID) {
		httpx.WriteError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}
