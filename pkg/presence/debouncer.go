package presence

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/userengine/presence/pkg/events"
	"github.com/userengine/presence/pkg/metrics"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// DebouncerConfig configures the debouncer behavior.
type DebouncerConfig struct {
	// DisconnectDelay is how long to wait before executing disconnect.
	// Default: 5s
	DisconnectDelay time.Duration

	// JobTTL is how long job metadata persists in Redis.
	// Should be longer than DisconnectDelay. Default: 60s
	JobTTL time.Duration

	// PollInterval is how often to check for due jobs (worker mode).
	// Default: 1s
	PollInterval time.Duration

	// BatchSize is max jobs to process per tick.
	// Default: 100
	BatchSize int64

	// FlappingWindow is the time window to track transitions for flapping detection.
	// Default: 30s
	FlappingWindow time.Duration

	// FlappingThreshold is the max transitions in FlappingWindow before suppressing.
	// Default: 5
	FlappingThreshold int

	// OfflineDelay is how long to wait for sustained offline before broadcasting.
	// Default: 15s
	OfflineDelay time.Duration
}

// DefaultDebouncerConfig returns sensible defaults.
func DefaultDebouncerConfig() DebouncerConfig {
	return DebouncerConfig{
		DisconnectDelay:   5 * time.Second,
		JobTTL:            60 * time.Second,
		PollInterval:      1 * time.Second,
		BatchSize:         100,
		FlappingWindow:    30 * time.Second,
		FlappingThreshold: 5,
		OfflineDelay:      15 * time.Second,
	}
}

// Debouncer handles delayed disconnect job execution.
// Can be used embedded or as a standalone worker.
type Debouncer struct {
	svc      *Service
	eventBus events.Publisher
	rdb      *redis.Client
	cfg      DebouncerConfig
	keys     Key
}

// NewDebouncer creates a new debouncer instance.
func NewDebouncer(svc *Service, eventBus events.Publisher, cfg DebouncerConfig) *Debouncer {
	return &Debouncer{
		svc:      svc,
		eventBus: eventBus,
		rdb:      svc.Redis(),
		cfg:      cfg,
		keys:     Key{},
	}
}

// ScheduleDisconnect creates a debounced disconnect job.
// Returns the job ID that can be used for cancellation.
func (d *Debouncer) ScheduleDisconnect(ctx context.Context, scopeID, userID, sessionID string) (string, error) {
	jobID := uuid.New().String()
	runAt := time.Now().Add(d.cfg.DisconnectDelay).UnixMilli()

	pipe := d.rdb.Pipeline()

	// Add job to queue
	pipe.ZAdd(ctx, d.keys.DisconnectJobs(scopeID), &redis.Z{
		Score:  float64(runAt),
		Member: jobID,
	})

	// Store job metadata with TTL
	metaKey := d.keys.DisconnectJobMeta(scopeID, jobID)
	pipe.HSet(ctx, metaKey, map[string]interface{}{
		"user_id":    userID,
		"session_id": sessionID,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	})
	pipe.Expire(ctx, metaKey, d.cfg.JobTTL)

	// Add to cancellation index
	pipe.SAdd(ctx, d.keys.DisconnectJobIndex(scopeID, userID, sessionID), jobID)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return "", err
	}

	// Track metrics
	metrics.Global().IncDebouncerJobScheduled()

	log.Debug().
		Str("scope_id", scopeID).
		Str("user_id", userID).
		Str("session_id", sessionID).
		Str("job_id", jobID).
		Time("run_at", time.UnixMilli(runAt)).
		Msg("disconnect job scheduled")

	return jobID, nil
}

// CancelDisconnect cancels pending disconnect jobs for a user/session.
// Call this on reconnect to prevent flicker.
func (d *Debouncer) CancelDisconnect(ctx context.Context, scopeID, userID, sessionID string) (int, error) {
	indexKey := d.keys.DisconnectJobIndex(scopeID, userID, sessionID)

	// Get all pending job IDs for this user/session
	jobIDs, err := d.rdb.SMembers(ctx, indexKey).Result()
	if err != nil {
		return 0, err
	}

	if len(jobIDs) == 0 {
		return 0, nil
	}

	pipe := d.rdb.Pipeline()
	jobsKey := d.keys.DisconnectJobs(scopeID)

	for _, jobID := range jobIDs {
		// Remove from queue
		pipe.ZRem(ctx, jobsKey, jobID)
		// Delete metadata
		pipe.Del(ctx, d.keys.DisconnectJobMeta(scopeID, jobID))
	}

	// Delete the index
	pipe.Del(ctx, indexKey)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}

	// Track metrics
	metrics.Global().IncDebouncerJobCancelled(len(jobIDs))

	log.Debug().
		Str("scope_id", scopeID).
		Str("user_id", userID).
		Str("session_id", sessionID).
		Int("cancelled", len(jobIDs)).
		Msg("disconnect jobs cancelled")

	return len(jobIDs), nil
}

// ProcessDueJobs executes all due disconnect jobs across all scopes.
// Call this periodically (e.g., every second) or use RunWorker.
func (d *Debouncer) ProcessDueJobs(ctx context.Context) error {
	// Get all active scopes
	scopes, err := d.rdb.SMembers(ctx, d.keys.Scopes()).Result()
	if err != nil {
		return err
	}

	for _, scopeID := range scopes {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := d.processScope(ctx, scopeID); err != nil {
			log.Error().Err(err).Str("scope_id", scopeID).Msg("process scope jobs failed")
		}
	}

	return nil
}

// RunWorker runs the debouncer as a continuous worker loop.
// Blocks until context is cancelled.
func (d *Debouncer) RunWorker(ctx context.Context) error {
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	log.Info().
		Dur("poll_interval", d.cfg.PollInterval).
		Dur("delay", d.cfg.DisconnectDelay).
		Msg("debouncer worker started")

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("debouncer worker stopped")
			return ctx.Err()
		case <-ticker.C:
			if err := d.ProcessDueJobs(ctx); err != nil {
				log.Error().Err(err).Msg("debouncer tick failed")
			}
		}
	}
}

func (d *Debouncer) processScope(ctx context.Context, scopeID string) error {
	jobsKey := d.keys.DisconnectJobs(scopeID)
	now := float64(time.Now().UnixMilli())

	// Get due jobs (score <= now), limit batch size
	jobs, err := d.rdb.ZRangeByScoreWithScores(ctx, jobsKey, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   strconv.FormatFloat(now, 'f', 0, 64),
		Count: d.cfg.BatchSize,
	}).Result()
	if err != nil {
		return err
	}

	for _, job := range jobs {
		jobID := job.Member.(string)
		if err := d.executeJob(ctx, scopeID, jobID); err != nil {
			log.Error().Err(err).
				Str("scope_id", scopeID).
				Str("job_id", jobID).
				Msg("execute job failed")
		}
	}

	return nil
}

// transitionKey returns the Redis key for tracking user transitions (for flapping detection).
func (d *Debouncer) transitionKey(scopeID, userID string) string {
	return fmt.Sprintf("presence:transitions:%s:%s", scopeID, userID)
}

// recordTransition records a transition and returns true if the user is flapping.
func (d *Debouncer) recordTransition(ctx context.Context, scopeID, userID string) (bool, error) {
	if d.cfg.FlappingThreshold <= 0 || d.cfg.FlappingWindow <= 0 {
		return false, nil // Flapping detection disabled
	}

	key := d.transitionKey(scopeID, userID)
	now := time.Now().UnixMilli()
	windowStart := now - d.cfg.FlappingWindow.Milliseconds()

	pipe := d.rdb.Pipeline()

	// Add current transition timestamp
	pipe.ZAdd(ctx, key, &redis.Z{
		Score:  float64(now),
		Member: strconv.FormatInt(now, 10),
	})

	// Remove old entries outside the window
	pipe.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(windowStart, 10))

	// Get count of transitions in window
	countCmd := pipe.ZCard(ctx, key)

	// Set TTL on the key
	pipe.Expire(ctx, key, d.cfg.FlappingWindow*2)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, err
	}

	count := countCmd.Val()
	isFlapping := int(count) >= d.cfg.FlappingThreshold

	if isFlapping {
		log.Warn().
			Str("scope_id", scopeID).
			Str("user_id", userID).
			Int64("transitions", count).
			Int("threshold", d.cfg.FlappingThreshold).
			Msg("flapping detected - suppressing offline event")
		metrics.Global().IncEventDropped() // Track suppressed events
	}

	return isFlapping, nil
}

// isUserFlapping checks if a user is currently flapping (without recording).
func (d *Debouncer) isUserFlapping(ctx context.Context, scopeID, userID string) (bool, error) {
	if d.cfg.FlappingThreshold <= 0 || d.cfg.FlappingWindow <= 0 {
		return false, nil
	}

	key := d.transitionKey(scopeID, userID)
	now := time.Now().UnixMilli()
	windowStart := now - d.cfg.FlappingWindow.Milliseconds()

	count, err := d.rdb.ZCount(ctx, key, strconv.FormatInt(windowStart, 10), "+inf").Result()
	if err != nil {
		return false, err
	}

	return int(count) >= d.cfg.FlappingThreshold, nil
}

func (d *Debouncer) executeJob(ctx context.Context, scopeID, jobID string) error {
	metaKey := d.keys.DisconnectJobMeta(scopeID, jobID)

	// Get job metadata
	meta, err := d.rdb.HGetAll(ctx, metaKey).Result()
	if err != nil {
		return err
	}

	// Job may have expired or been cancelled
	if len(meta) == 0 {
		// Clean up orphaned queue entry
		d.rdb.ZRem(ctx, d.keys.DisconnectJobs(scopeID), jobID)
		return nil
	}

	userID := meta["user_id"]
	sessionID := meta["session_id"]

	// Execute atomic disconnect
	result, err := d.svc.Disconnect(ctx, scopeID, sessionID, userID)
	if err != nil {
		return err
	}

	// Record transition for flapping detection
	isFlapping, _ := d.recordTransition(ctx, scopeID, userID)

	// Emit offline event if this was a true transition (unless flapping)
	if result.Transition == "offline" {
		if isFlapping {
			log.Debug().
				Str("scope_id", scopeID).
				Str("user_id", userID).
				Msg("offline event suppressed due to flapping")
		} else {
			event := events.PresenceEvent{
				EventID:    events.NewEventID(),
				ScopeID:    scopeID,
				Type:       events.EventTypeUserOffline,
				UserID:     userID,
				Version:    result.Version,
				TabCount:   result.SessionCount,
				OccurredAt: time.Now().UTC(),
				Source:     "debouncer",
			}

			if err := d.eventBus.Publish(ctx, scopeID, event); err != nil {
				log.Error().Err(err).
					Str("scope_id", scopeID).
					Str("user_id", userID).
					Msg("failed to publish offline event")
			}
		}
	} else if result.SessionCount > 0 {
		// Tab count changes are always published (not affected by flapping)
		event := events.PresenceEvent{
			EventID:    events.NewEventID(),
			ScopeID:    scopeID,
			Type:       events.EventTypeUserTabs,
			UserID:     userID,
			Version:    result.Version,
			TabCount:   result.SessionCount,
			OccurredAt: time.Now().UTC(),
			Source:     "debouncer",
		}

		if err := d.eventBus.Publish(ctx, scopeID, event); err != nil {
			log.Error().Err(err).
				Str("scope_id", scopeID).
				Str("user_id", userID).
				Msg("failed to publish tab count event")
		}
	}

	// Cleanup
	pipe := d.rdb.Pipeline()
	pipe.ZRem(ctx, d.keys.DisconnectJobs(scopeID), jobID)
	pipe.Del(ctx, metaKey)
	pipe.SRem(ctx, d.keys.DisconnectJobIndex(scopeID, userID, sessionID), jobID)
	pipe.Exec(ctx)

	// Track metrics
	metrics.Global().IncDebouncerJobExecuted()

	log.Debug().
		Str("scope_id", scopeID).
		Str("user_id", userID).
		Str("session_id", sessionID).
		Str("job_id", jobID).
		Str("transition", result.Transition).
		Bool("flapping", isFlapping).
		Msg("disconnect job executed")

	return nil
}
