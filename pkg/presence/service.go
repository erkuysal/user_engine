package presence

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/rs/zerolog/log"
)

//go:embed lua/connect.lua
var connectScript string

//go:embed lua/heartbeat.lua
var heartbeatScript string

//go:embed lua/disconnect.lua
var disconnectScript string

var (
	ErrSessionUnknown  = errors.New("session unknown")
	ErrScriptNotLoaded = errors.New("lua script not loaded")
)

// TransitionResult represents the result of a presence transition.
type TransitionResult struct {
	Transition string `json:"transition"` // "online", "offline", or "none"
	Version    int64  `json:"version"`    // monotonic version for deduplication
	// SessionCount is the current number of sessions (tabs) for the user.
	SessionCount int64 `json:"session_count"`
}

// SessionMeta holds session metadata (write-once at connect).
type SessionMeta struct {
	UserID      string    `json:"user_id"`
	SessionID   string    `json:"session_id"`
	ScopeID     string    `json:"scope_id"`
	ConnectedAt time.Time `json:"connected_at"`
	UserAgent   string    `json:"user_agent,omitempty"`
	IPAddress   string    `json:"ip_address,omitempty"`
	DeviceID    string    `json:"device_id,omitempty"`
	DeviceType  string    `json:"device_type,omitempty"` // "desktop", "mobile", "tablet"
	Invisible   bool      `json:"invisible,omitempty"`   // Ghost mode - receive events but appear offline
}

// Service provides the presence domain API.
// This is the core component that can be embedded in your application.
type Service struct {
	rdb        *redis.Client
	keys       Key
	sessionTTL time.Duration

	// Cached script SHAs
	connectSHA    string
	heartbeatSHA  string
	disconnectSHA string
}

// NewService creates a new presence service.
func NewService(rdb *redis.Client) *Service {
	return &Service{
		rdb:        rdb,
		keys:       Key{UseHashTags: false},
		sessionTTL: 45 * time.Second,
	}
}

// NewServiceWithCluster creates a new presence service configured for Redis Cluster.
func NewServiceWithCluster(rdb *redis.Client, useHashTags bool) *Service {
	return &Service{
		rdb:        rdb,
		keys:       Key{UseHashTags: useHashTags},
		sessionTTL: 45 * time.Second,
	}
}

// SetSessionTTL configures the session TTL.
func (s *Service) SetSessionTTL(ttl time.Duration) {
	s.sessionTTL = ttl
}

// SetUseHashTags enables or disables Redis Cluster hash tags.
// Must be called before any operations.
func (s *Service) SetUseHashTags(useHashTags bool) {
	s.keys = Key{UseHashTags: useHashTags}
}

// SessionTTL returns the current session TTL.
func (s *Service) SessionTTL() time.Duration {
	return s.sessionTTL
}

// LoadScripts loads and caches Lua script SHAs.
// Must be called before using Connect/Heartbeat/Disconnect.
func (s *Service) LoadScripts(ctx context.Context) error {
	var err error

	s.connectSHA, err = s.rdb.ScriptLoad(ctx, connectScript).Result()
	if err != nil {
		return err
	}

	s.heartbeatSHA, err = s.rdb.ScriptLoad(ctx, heartbeatScript).Result()
	if err != nil {
		return err
	}

	s.disconnectSHA, err = s.rdb.ScriptLoad(ctx, disconnectScript).Result()
	if err != nil {
		return err
	}

	return nil
}

// Connect establishes a new session for a user.
// Returns the transition result (online if this is the user's first session).
// If meta.Invisible is true, the user will receive events but appear offline to others.
func (s *Service) Connect(ctx context.Context, meta SessionMeta) (*TransitionResult, error) {
	if s.connectSHA == "" {
		log.Error().Msg("[DEBUG] Connect called but scripts not loaded!")
		return nil, ErrScriptNotLoaded
	}

	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}

	sessionKey := s.keys.Session(meta.ScopeID, meta.SessionID)
	ttlSeconds := int(s.sessionTTL.Seconds())

	log.Info().
		Str("user_id", meta.UserID).
		Str("session_id", meta.SessionID).
		Str("scope_id", meta.ScopeID).
		Str("session_key", sessionKey).
		Int("ttl_seconds", ttlSeconds).
		Dur("session_ttl", s.sessionTTL).
		Msg("[DEBUG] Connect: creating new session in Redis")

	keys := []string{
		sessionKey,
		s.keys.UserSessions(meta.ScopeID, meta.UserID),
		s.keys.OnlineUsers(meta.ScopeID),
		s.keys.UserVersion(meta.ScopeID, meta.UserID),
		s.keys.Scopes(),
	}

	args := []interface{}{
		string(metaJSON),
		ttlSeconds,
		meta.SessionID,
		meta.UserID,
		meta.ScopeID,
	}

	result, err := s.rdb.EvalSha(ctx, s.connectSHA, keys, args...).Result()
	if err != nil {
		log.Error().
			Err(err).
			Str("user_id", meta.UserID).
			Str("session_id", meta.SessionID).
			Msg("[DEBUG] Connect: Redis EvalSha failed")
		return nil, err
	}

	transitionResult, err := parseTransitionResult(result)
	if err != nil {
		return nil, err
	}

	log.Info().
		Str("user_id", meta.UserID).
		Str("session_id", meta.SessionID).
		Str("transition", transitionResult.Transition).
		Int64("session_count", transitionResult.SessionCount).
		Int64("version", transitionResult.Version).
		Int("ttl_seconds", ttlSeconds).
		Msg("[DEBUG] Connect: session created successfully")

	// Handle invisible mode
	if meta.Invisible {
		// Add user to invisible set
		s.rdb.SAdd(ctx, s.keys.InvisibleUsers(meta.ScopeID), meta.UserID)

		// For invisible users, don't report online transition to others
		if transitionResult.Transition == "online" {
			transitionResult.Transition = "invisible"
		}
	}

	// Track device type if provided
	if meta.DeviceType != "" {
		s.rdb.HIncrBy(ctx, s.keys.UserDevices(meta.ScopeID, meta.UserID), meta.DeviceType, 1)
	}

	return transitionResult, nil
}

// Heartbeat extends a session's TTL.
// Returns ErrSessionUnknown if session is expired or doesn't exist.
func (s *Service) Heartbeat(ctx context.Context, scopeID, sessionID string) error {
	if s.heartbeatSHA == "" {
		log.Error().Msg("[DEBUG] Heartbeat called but scripts not loaded!")
		return ErrScriptNotLoaded
	}

	sessionKey := s.keys.Session(scopeID, sessionID)
	ttlSeconds := int(s.sessionTTL.Seconds())

	log.Debug().
		Str("session_id", sessionID).
		Str("scope_id", scopeID).
		Str("session_key", sessionKey).
		Int("ttl_seconds", ttlSeconds).
		Msg("[DEBUG] Heartbeat: refreshing session TTL in Redis")

	keys := []string{sessionKey}
	args := []interface{}{ttlSeconds}

	result, err := s.rdb.EvalSha(ctx, s.heartbeatSHA, keys, args...).Result()
	if err != nil {
		log.Error().
			Err(err).
			Str("session_id", sessionID).
			Str("session_key", sessionKey).
			Msg("[DEBUG] Heartbeat: Redis EvalSha failed")
		return err
	}

	log.Debug().
		Str("session_id", sessionID).
		Interface("result", result).
		Msg("[DEBUG] Heartbeat: Lua script result")

	if result == "UNKNOWN" {
		log.Warn().
			Str("session_id", sessionID).
			Str("session_key", sessionKey).
			Msg("[DEBUG] Heartbeat: session not found in Redis - EXPIRED or NEVER EXISTED")
		return ErrSessionUnknown
	}

	log.Debug().
		Str("session_id", sessionID).
		Int("new_ttl", ttlSeconds).
		Msg("[DEBUG] Heartbeat: session TTL refreshed successfully")

	return nil
}

// Disconnect removes a session.
// Returns the transition result (offline if this was the user's last session).
func (s *Service) Disconnect(ctx context.Context, scopeID, sessionID, userID string) (*TransitionResult, error) {
	if s.disconnectSHA == "" {
		return nil, ErrScriptNotLoaded
	}

	// Get session metadata to check device type before disconnect
	sessionKey := s.keys.Session(scopeID, sessionID)
	sessionData, _ := s.rdb.Get(ctx, sessionKey).Result()

	var deviceType string
	if sessionData != "" {
		var meta SessionMeta
		if json.Unmarshal([]byte(sessionData), &meta) == nil {
			deviceType = meta.DeviceType
		}
	}

	keys := []string{
		sessionKey,
		s.keys.UserSessions(scopeID, userID),
		s.keys.OnlineUsers(scopeID),
		s.keys.UserVersion(scopeID, userID),
	}

	args := []interface{}{
		sessionID,
		userID,
	}

	result, err := s.rdb.EvalSha(ctx, s.disconnectSHA, keys, args...).Result()
	if err != nil {
		return nil, err
	}

	transitionResult, err := parseTransitionResult(result)
	if err != nil {
		return nil, err
	}

	// If user went offline, clean up invisible status and device tracking
	if transitionResult.Transition == "offline" {
		s.rdb.SRem(ctx, s.keys.InvisibleUsers(scopeID), userID)
		s.rdb.Del(ctx, s.keys.UserDevices(scopeID, userID))
	} else if deviceType != "" {
		// Decrement device count
		s.rdb.HIncrBy(ctx, s.keys.UserDevices(scopeID, userID), deviceType, -1)
	}

	return transitionResult, nil
}

// DisconnectUser removes all sessions for a user within a scope.
// Returns the final transition result (offline if this removed the last session).
func (s *Service) DisconnectUser(ctx context.Context, scopeID, userID string) (*TransitionResult, error) {
	if s.disconnectSHA == "" {
		return nil, ErrScriptNotLoaded
	}

	userSessionsKey := s.keys.UserSessions(scopeID, userID)
	sessions, err := s.rdb.SMembers(ctx, userSessionsKey).Result()
	if err != nil {
		return nil, err
	}

	if len(sessions) == 0 {
		onlineKey := s.keys.OnlineUsers(scopeID)
		versionKey := s.keys.UserVersion(scopeID, userID)

		removed, err := s.rdb.SRem(ctx, onlineKey, userID).Result()
		if err != nil {
			return nil, err
		}
		if removed > 0 {
			version, err := s.rdb.Incr(ctx, versionKey).Result()
			if err != nil {
				return nil, err
			}
			return &TransitionResult{Transition: "offline", Version: version}, nil
		}

		version, _ := s.GetUserVersion(ctx, scopeID, userID)
		return &TransitionResult{Transition: "none", Version: version}, nil
	}

	var lastResult *TransitionResult
	for _, sessionID := range sessions {
		result, err := s.Disconnect(ctx, scopeID, sessionID, userID)
		if err != nil {
			return nil, err
		}
		lastResult = result
	}

	if lastResult == nil {
		return &TransitionResult{Transition: "none", Version: 0}, nil
	}

	return lastResult, nil
}

// GetOnlineUsers returns online users for a scope using SSCAN pagination.
func (s *Service) GetOnlineUsers(ctx context.Context, scopeID, cursor string, count int64) ([]string, string, error) {
	key := s.keys.OnlineUsers(scopeID)
	users, nextCursor, err := s.rdb.SScan(ctx, key, parseCursor(cursor), "", count).Result()
	if err != nil {
		return nil, "", err
	}
	return users, formatCursor(nextCursor), nil
}

// LookupUsers checks if specific users are online in a scope.
// Users in invisible mode will appear as "offline" to others.
func (s *Service) LookupUsers(ctx context.Context, scopeID string, userIDs []string) (map[string]string, error) {
	onlineKey := s.keys.OnlineUsers(scopeID)
	invisibleKey := s.keys.InvisibleUsers(scopeID)

	pipe := s.rdb.Pipeline()
	onlineCmds := make([]*redis.BoolCmd, len(userIDs))
	invisibleCmds := make([]*redis.BoolCmd, len(userIDs))

	for i, userID := range userIDs {
		onlineCmds[i] = pipe.SIsMember(ctx, onlineKey, userID)
		invisibleCmds[i] = pipe.SIsMember(ctx, invisibleKey, userID)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	statuses := make(map[string]string, len(userIDs))
	for i, userID := range userIDs {
		isOnline := onlineCmds[i].Val()
		isInvisible := invisibleCmds[i].Val()

		if isOnline && !isInvisible {
			statuses[userID] = "online"
		} else {
			statuses[userID] = "offline"
		}
	}

	return statuses, nil
}

// LookupUsersWithInvisible checks if specific users are online, including invisible status.
// Returns "online", "invisible", or "offline" for each user.
// Use this for admin/debug purposes.
func (s *Service) LookupUsersWithInvisible(ctx context.Context, scopeID string, userIDs []string) (map[string]string, error) {
	onlineKey := s.keys.OnlineUsers(scopeID)
	invisibleKey := s.keys.InvisibleUsers(scopeID)

	pipe := s.rdb.Pipeline()
	onlineCmds := make([]*redis.BoolCmd, len(userIDs))
	invisibleCmds := make([]*redis.BoolCmd, len(userIDs))

	for i, userID := range userIDs {
		onlineCmds[i] = pipe.SIsMember(ctx, onlineKey, userID)
		invisibleCmds[i] = pipe.SIsMember(ctx, invisibleKey, userID)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	statuses := make(map[string]string, len(userIDs))
	for i, userID := range userIDs {
		isOnline := onlineCmds[i].Val()
		isInvisible := invisibleCmds[i].Val()

		if isOnline && isInvisible {
			statuses[userID] = "invisible"
		} else if isOnline {
			statuses[userID] = "online"
		} else {
			statuses[userID] = "offline"
		}
	}

	return statuses, nil
}

// GetUserSessionCount returns the number of active sessions for a user.
func (s *Service) GetUserSessionCount(ctx context.Context, scopeID, userID string) (int64, error) {
	key := s.keys.UserSessions(scopeID, userID)
	return s.rdb.SCard(ctx, key).Result()
}

// GetUsersSessionCounts returns session counts for multiple users.
func (s *Service) GetUsersSessionCounts(ctx context.Context, scopeID string, userIDs []string) (map[string]int64, error) {
	pipe := s.rdb.Pipeline()
	cmds := make([]*redis.IntCmd, len(userIDs))

	for i, userID := range userIDs {
		cmds[i] = pipe.SCard(ctx, s.keys.UserSessions(scopeID, userID))
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int64, len(userIDs))
	for i, userID := range userIDs {
		counts[userID] = cmds[i].Val()
	}

	return counts, nil
}

// IsUserOnline checks if a single user is online in a scope.
func (s *Service) IsUserOnline(ctx context.Context, scopeID, userID string) (bool, error) {
	key := s.keys.OnlineUsers(scopeID)
	return s.rdb.SIsMember(ctx, key, userID).Result()
}

// GetUserVersion returns the current version for a user.
func (s *Service) GetUserVersion(ctx context.Context, scopeID, userID string) (int64, error) {
	key := s.keys.UserVersion(scopeID, userID)
	val, err := s.rdb.Get(ctx, key).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	return val, err
}

// GetActiveScopes returns all active scope IDs.
func (s *Service) GetActiveScopes(ctx context.Context) ([]string, error) {
	return s.rdb.SMembers(ctx, s.keys.Scopes()).Result()
}

// UserDeviceInfo represents device presence information for a user.
type UserDeviceInfo struct {
	UserID        string           `json:"user_id"`
	Online        bool             `json:"online"`
	Devices       map[string]int64 `json:"devices"`        // device_type -> session_count
	PrimaryDevice string           `json:"primary_device"` // Highest priority device
	TotalSessions int64            `json:"total_sessions"`
}

// GetUserDevices returns device information for a user in a scope.
func (s *Service) GetUserDevices(ctx context.Context, scopeID, userID string) (*UserDeviceInfo, error) {
	pipe := s.rdb.Pipeline()

	isOnlineCmd := pipe.SIsMember(ctx, s.keys.OnlineUsers(scopeID), userID)
	devicesCmd := pipe.HGetAll(ctx, s.keys.UserDevices(scopeID, userID))
	sessionCountCmd := pipe.SCard(ctx, s.keys.UserSessions(scopeID, userID))

	_, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	devices := make(map[string]int64)
	for deviceType, countStr := range devicesCmd.Val() {
		count, err := parseInt64(countStr)
		if err == nil && count > 0 {
			devices[deviceType] = count
		}
	}

	info := &UserDeviceInfo{
		UserID:        userID,
		Online:        isOnlineCmd.Val(),
		Devices:       devices,
		PrimaryDevice: derivePrimaryDevice(devices),
		TotalSessions: sessionCountCmd.Val(),
	}

	return info, nil
}

// GetUsersDevices returns device information for multiple users.
func (s *Service) GetUsersDevices(ctx context.Context, scopeID string, userIDs []string) (map[string]*UserDeviceInfo, error) {
	if len(userIDs) == 0 {
		return make(map[string]*UserDeviceInfo), nil
	}

	pipe := s.rdb.Pipeline()

	type userCmds struct {
		isOnline     *redis.BoolCmd
		devices      *redis.StringStringMapCmd
		sessionCount *redis.IntCmd
	}

	cmds := make([]userCmds, len(userIDs))
	for i, userID := range userIDs {
		cmds[i] = userCmds{
			isOnline:     pipe.SIsMember(ctx, s.keys.OnlineUsers(scopeID), userID),
			devices:      pipe.HGetAll(ctx, s.keys.UserDevices(scopeID, userID)),
			sessionCount: pipe.SCard(ctx, s.keys.UserSessions(scopeID, userID)),
		}
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*UserDeviceInfo, len(userIDs))
	for i, userID := range userIDs {
		devices := make(map[string]int64)
		for deviceType, countStr := range cmds[i].devices.Val() {
			count, err := parseInt64(countStr)
			if err == nil && count > 0 {
				devices[deviceType] = count
			}
		}

		result[userID] = &UserDeviceInfo{
			UserID:        userID,
			Online:        cmds[i].isOnline.Val(),
			Devices:       devices,
			PrimaryDevice: derivePrimaryDevice(devices),
			TotalSessions: cmds[i].sessionCount.Val(),
		}
	}

	return result, nil
}

// derivePrimaryDevice determines the primary device based on priority.
// Priority: desktop > tablet > mobile > unknown
func derivePrimaryDevice(devices map[string]int64) string {
	priority := []string{"desktop", "tablet", "mobile", "unknown"}

	for _, deviceType := range priority {
		if count, ok := devices[deviceType]; ok && count > 0 {
			return deviceType
		}
	}

	// Return the first device type found if none match priority
	for deviceType, count := range devices {
		if count > 0 {
			return deviceType
		}
	}

	return ""
}

func parseInt64(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid number")
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}

// Keys returns the key generator for direct Redis access.
func (s *Service) Keys() Key {
	return s.keys
}

// Redis returns the underlying Redis client.
func (s *Service) Redis() *redis.Client {
	return s.rdb
}

func parseTransitionResult(result interface{}) (*TransitionResult, error) {
	arr, ok := result.([]interface{})
	if !ok || (len(arr) != 2 && len(arr) != 3) {
		return nil, errors.New("unexpected lua result format")
	}

	transition, ok := arr[0].(string)
	if !ok {
		return nil, errors.New("unexpected transition type")
	}

	version, ok := arr[1].(int64)
	if !ok {
		return nil, errors.New("unexpected version type")
	}

	var sessionCount int64
	if len(arr) == 3 {
		if v, ok := arr[2].(int64); ok {
			sessionCount = v
		} else {
			return nil, errors.New("unexpected session_count type")
		}
	}

	return &TransitionResult{
		Transition:   transition,
		Version:      version,
		SessionCount: sessionCount,
	}, nil
}

func parseCursor(cursor string) uint64 {
	if cursor == "" || cursor == "0" {
		return 0
	}
	var c uint64
	_ = json.Unmarshal([]byte(cursor), &c)
	return c
}

func formatCursor(cursor uint64) string {
	if cursor == 0 {
		return "0"
	}
	b, _ := json.Marshal(cursor)
	return string(b)
}
