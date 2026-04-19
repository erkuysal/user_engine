package presence

import (
	"context"
	"errors"
	"strings"

	"github.com/go-redis/redis/v8"
)

// Per-session presence values (WebSocket / client).
const (
	PresenceStateActive = "active"
	PresenceStateIdle   = "idle"
	PresenceStateDnd    = "dnd"
)

// ErrInvalidPresenceState is returned for unknown state strings.
var ErrInvalidPresenceState = errors.New("invalid presence state")

// PresenceStateUpdateResult is returned when a client updates presence via WebSocket.
type PresenceStateUpdateResult struct {
	Version   int64
	TabCount  int64
	Aggregate string
	Published bool
}

func normalizePresenceState(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case PresenceStateActive, "online":
		return PresenceStateActive, true
	case PresenceStateIdle:
		return PresenceStateIdle, true
	case PresenceStateDnd, "do_not_disturb":
		return PresenceStateDnd, true
	default:
		return "", false
	}
}

func aggregateSessionStates(states map[string]string) string {
	hasDnd, hasIdle := false, false
	for _, s := range states {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case PresenceStateDnd:
			hasDnd = true
		case PresenceStateIdle:
			hasIdle = true
		}
	}
	if hasDnd {
		return PresenceStateDnd
	}
	if hasIdle {
		return PresenceStateIdle
	}
	return PresenceStateActive
}

// GetPresenceAggregate returns the merged presence for an online user from Redis.
func (s *Service) GetPresenceAggregate(ctx context.Context, scopeID, userID string) (string, error) {
	key := s.keys.UserSessionStates(scopeID, userID)
	m, err := s.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return "", err
	}
	if len(m) == 0 {
		return PresenceStateActive, nil
	}
	return aggregateSessionStates(m), nil
}

// InitSessionPresenceOnConnect registers default active state for a new WebSocket session.
func (s *Service) InitSessionPresenceOnConnect(ctx context.Context, scopeID, userID, sessionID string, sessionCount int64) error {
	statesKey := s.keys.UserSessionStates(scopeID, userID)
	lastKey := s.keys.UserLastPublishedPresence(scopeID, userID)

	if err := s.rdb.HSet(ctx, statesKey, sessionID, PresenceStateActive).Err(); err != nil {
		return err
	}

	if sessionCount == 1 {
		return s.rdb.Set(ctx, lastKey, PresenceStateActive, 0).Err()
	}
	return nil
}

// SetSessionPresenceState updates this tab's presence and, if the user-level aggregate changed,
// bumps version and reports Published=true for the gateway to emit user_presence.
func (s *Service) SetSessionPresenceState(ctx context.Context, scopeID, userID, sessionID, rawState string) (*PresenceStateUpdateResult, error) {
	state, ok := normalizePresenceState(rawState)
	if !ok {
		return nil, ErrInvalidPresenceState
	}

	userSessionsKey := s.keys.UserSessions(scopeID, userID)
	isMember, err := s.rdb.SIsMember(ctx, userSessionsKey, sessionID).Result()
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrSessionUnknown
	}

	statesKey := s.keys.UserSessionStates(scopeID, userID)
	lastKey := s.keys.UserLastPublishedPresence(scopeID, userID)
	versionKey := s.keys.UserVersion(scopeID, userID)

	if err := s.rdb.HSet(ctx, statesKey, sessionID, state).Err(); err != nil {
		return nil, err
	}

	m, err := s.rdb.HGetAll(ctx, statesKey).Result()
	if err != nil {
		return nil, err
	}
	agg := aggregateSessionStates(m)

	last, err := s.rdb.Get(ctx, lastKey).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}
	if err == redis.Nil {
		last = ""
	}

	if agg == last {
		tabCount, err := s.rdb.SCard(ctx, userSessionsKey).Result()
		if err != nil {
			return nil, err
		}
		v, _ := s.rdb.Get(ctx, versionKey).Int64()
		return &PresenceStateUpdateResult{
			Version:   v,
			TabCount:  tabCount,
			Aggregate: agg,
			Published: false,
		}, nil
	}

	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, lastKey, agg, 0)
	pipe.Incr(ctx, versionKey)
	pipe.SCard(ctx, userSessionsKey)
	cmds, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	v, err := cmds[1].(*redis.IntCmd).Result()
	if err != nil {
		return nil, err
	}
	tabCount, err := cmds[2].(*redis.IntCmd).Result()
	if err != nil {
		return nil, err
	}

	return &PresenceStateUpdateResult{
		Version:   v,
		TabCount:  tabCount,
		Aggregate: agg,
		Published: true,
	}, nil
}

// applyDisconnectPresenceState removes the session from per-tab state, updates last-published
// when the aggregate changes, and returns the new aggregate for event payloads (if still online).
func (s *Service) applyDisconnectPresenceState(ctx context.Context, scopeID, userID, sessionID string, transitionOffline bool) (aggregate string, presenceChanged bool, err error) {
	statesKey := s.keys.UserSessionStates(scopeID, userID)
	lastKey := s.keys.UserLastPublishedPresence(scopeID, userID)

	if transitionOffline {
		_ = s.rdb.Del(ctx, statesKey, lastKey).Err()
		return "", false, nil
	}

	if err := s.rdb.HDel(ctx, statesKey, sessionID).Err(); err != nil {
		return "", false, err
	}

	m, err := s.rdb.HGetAll(ctx, statesKey).Result()
	if err != nil {
		return "", false, err
	}
	agg := aggregateSessionStates(m)

	last, err := s.rdb.Get(ctx, lastKey).Result()
	if err != nil && err != redis.Nil {
		return "", false, err
	}
	if err == redis.Nil {
		last = ""
	}

	if agg == last {
		return agg, false, nil
	}

	if err := s.rdb.Set(ctx, lastKey, agg, 0).Err(); err != nil {
		return "", false, err
	}

	return agg, true, nil
}
