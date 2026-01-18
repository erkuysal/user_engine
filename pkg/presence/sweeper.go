package presence

import (
	"context"
	"time"

	"github.com/userengine/presence/pkg/events"

	"github.com/rs/zerolog/log"
)

// SweeperConfig configures the sweeper behavior.
type SweeperConfig struct {
	// Interval is how often the sweeper runs a tick.
	// Default: 30s
	Interval time.Duration

	// BudgetPerScope is max wall time per scope per tick.
	// Default: 100ms (50-200ms recommended)
	BudgetPerScope time.Duration

	// ScanCount is SSCAN COUNT hint per iteration.
	// Default: 100
	ScanCount int64
}

// DefaultSweeperConfig returns sensible defaults.
func DefaultSweeperConfig() SweeperConfig {
	return SweeperConfig{
		Interval:       30 * time.Second,
		BudgetPerScope: 100 * time.Millisecond,
		ScanCount:      100,
	}
}

// Sweeper cleans up zombie sessions using SSCAN with time budgets.
// Can be used embedded or as a standalone worker.
type Sweeper struct {
	svc      *Service
	eventBus events.Publisher
	cfg      SweeperConfig
}

// NewSweeper creates a new sweeper instance.
func NewSweeper(svc *Service, eventBus events.Publisher, cfg SweeperConfig) *Sweeper {
	return &Sweeper{
		svc:      svc,
		eventBus: eventBus,
		cfg:      cfg,
	}
}

// Tick performs one sweep iteration across all scopes.
// Call this periodically or use RunWorker.
func (s *Sweeper) Tick(ctx context.Context) error {
	rdb := s.svc.Redis()
	keys := s.svc.Keys()

	// Get all active scopes
	scopes, err := rdb.SMembers(ctx, keys.Scopes()).Result()
	if err != nil {
		return err
	}

	for _, scopeID := range scopes {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := s.sweepScope(ctx, scopeID); err != nil {
			log.Error().Err(err).Str("scope_id", scopeID).Msg("sweep scope failed")
			// Continue to next scope
		}
	}

	return nil
}

// RunWorker runs the sweeper as a continuous worker loop.
// Blocks until context is cancelled.
func (s *Sweeper) RunWorker(ctx context.Context) error {
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	log.Info().
		Dur("interval", s.cfg.Interval).
		Dur("budget", s.cfg.BudgetPerScope).
		Msg("sweeper worker started")

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("sweeper worker stopped")
			return ctx.Err()
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil {
				log.Error().Err(err).Msg("sweeper tick failed")
			}
		}
	}
}

func (s *Sweeper) sweepScope(ctx context.Context, scopeID string) error {
	rdb := s.svc.Redis()
	keys := s.svc.Keys()
	budget := s.cfg.BudgetPerScope
	startTime := time.Now()

	// Load cursor from persistence
	cursorKey := keys.SweepCursor(scopeID)
	cursor, _ := rdb.Get(ctx, cursorKey).Uint64()

	onlineUsersKey := keys.OnlineUsers(scopeID)
	prunedSessions := 0
	offlineTransitions := 0

	for {
		// Check time budget
		if time.Since(startTime) > budget {
			// Save cursor for next tick
			rdb.Set(ctx, cursorKey, cursor, 0)
			log.Debug().
				Str("scope_id", scopeID).
				Uint64("cursor", cursor).
				Int("pruned_sessions", prunedSessions).
				Msg("sweep paused (budget exceeded)")
			return nil
		}

		// Scan batch of online users
		users, nextCursor, err := rdb.SScan(ctx, onlineUsersKey, cursor, "", s.cfg.ScanCount).Result()
		if err != nil {
			return err
		}

		for _, userID := range users {
			pruned, wentOffline, err := s.sweepUser(ctx, scopeID, userID)
			if err != nil {
				log.Error().Err(err).
					Str("scope_id", scopeID).
					Str("user_id", userID).
					Msg("sweep user failed")
				continue
			}
			prunedSessions += pruned
			if wentOffline {
				offlineTransitions++
			}
		}

		cursor = nextCursor

		// Full scan complete
		if cursor == 0 {
			// Delete cursor (reset for next full scan)
			rdb.Del(ctx, cursorKey)

			// Check if scope is now empty
			count, err := rdb.SCard(ctx, onlineUsersKey).Result()
			if err == nil && count == 0 {
				// Remove scope from active scopes
				rdb.SRem(ctx, keys.Scopes(), scopeID)
				log.Info().Str("scope_id", scopeID).Msg("scope removed (empty)")
			}

			log.Debug().
				Str("scope_id", scopeID).
				Int("pruned_sessions", prunedSessions).
				Int("offline_transitions", offlineTransitions).
				Dur("elapsed", time.Since(startTime)).
				Msg("sweep complete")
			return nil
		}
	}
}

func (s *Sweeper) sweepUser(ctx context.Context, scopeID, userID string) (prunedCount int, wentOffline bool, err error) {
	rdb := s.svc.Redis()
	keys := s.svc.Keys()

	userSessionsKey := keys.UserSessions(scopeID, userID)

	// Get all sessions for this user
	sessions, err := rdb.SMembers(ctx, userSessionsKey).Result()
	if err != nil {
		return 0, false, err
	}

	// Check each session
	for _, sessionID := range sessions {
		sessionKey := keys.Session(scopeID, sessionID)
		exists, err := rdb.Exists(ctx, sessionKey).Result()
		if err != nil {
			continue
		}

		if exists == 0 {
			// Session expired, remove from user's session set
			rdb.SRem(ctx, userSessionsKey, sessionID)
			prunedCount++
		}
	}

	// Check if user has any sessions left
	remaining, err := rdb.SCard(ctx, userSessionsKey).Result()
	if err != nil {
		return prunedCount, false, err
	}

	if remaining == 0 {
		// User went offline
		onlineUsersKey := keys.OnlineUsers(scopeID)
		versionKey := keys.UserVersion(scopeID, userID)

		rdb.SRem(ctx, onlineUsersKey, userID)
		version, _ := rdb.Incr(ctx, versionKey).Result()

		// Emit offline event
		event := events.PresenceEvent{
			EventID:    events.NewEventID(),
			ScopeID:    scopeID,
			Type:       events.EventTypeUserOffline,
			UserID:     userID,
			Version:    version,
			TabCount:   0,
			OccurredAt: time.Now().UTC(),
			Source:     "sweeper",
		}

		if err := s.eventBus.Publish(ctx, scopeID, event); err != nil {
			log.Error().Err(err).
				Str("scope_id", scopeID).
				Str("user_id", userID).
				Msg("failed to publish offline event")
		}

		wentOffline = true
	} else if prunedCount > 0 {
		versionKey := keys.UserVersion(scopeID, userID)
		version, _ := rdb.Incr(ctx, versionKey).Result()

		event := events.PresenceEvent{
			EventID:    events.NewEventID(),
			ScopeID:    scopeID,
			Type:       events.EventTypeUserTabs,
			UserID:     userID,
			Version:    version,
			TabCount:   int64(remaining),
			OccurredAt: time.Now().UTC(),
			Source:     "sweeper",
		}

		if err := s.eventBus.Publish(ctx, scopeID, event); err != nil {
			log.Error().Err(err).
				Str("scope_id", scopeID).
				Str("user_id", userID).
				Msg("failed to publish tab count event")
		}
	}

	return prunedCount, wentOffline, nil
}
