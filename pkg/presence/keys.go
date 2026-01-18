package presence

import "fmt"

// Key provides scoped Redis key generation for presence data.
// All keys are prefixed with "presence:" and scoped by scope_id.
//
// For Redis Cluster compatibility, related keys use hash tags {scope_id}
// to ensure they land on the same shard. This is required for multi-key
// operations like Lua scripts that access multiple keys.
type Key struct {
	// UseHashTags enables Redis Cluster hash tag mode.
	// When true, keys include {scope_id} or {scope_id:user_id} hash tags.
	UseHashTags bool
}

// NewKey creates a new Key generator.
func NewKey(useHashTags bool) Key {
	return Key{UseHashTags: useHashTags}
}

// hashTag wraps the tag in curly braces for Redis Cluster key hashing.
func (k Key) hashTag(tag string) string {
	if k.UseHashTags {
		return "{" + tag + "}"
	}
	return tag
}

// Session returns the key for a session's metadata.
// Value: opaque blob (JSON), write-once at connect, TTL=45s
// Hash tag: {scope_id:user_id} for user-scoped operations
func (k Key) Session(scopeID, sessionID string) string {
	tag := k.hashTag(scopeID)
	return fmt.Sprintf("presence:session:%s:%s", tag, sessionID)
}

// UserSessions returns the key for a user's session set.
// Value: SET of session_id
// Hash tag: {scope_id:user_id} for user-scoped operations
func (k Key) UserSessions(scopeID, userID string) string {
	tag := k.hashTag(scopeID + ":" + userID)
	return fmt.Sprintf("presence:user_sessions:%s", tag)
}

// OnlineUsers returns the key for a scope's online users set.
// Value: SET of user_id
// Hash tag: {scope_id} for scope-scoped operations
func (k Key) OnlineUsers(scopeID string) string {
	tag := k.hashTag(scopeID)
	return fmt.Sprintf("presence:online_users:%s", tag)
}

// UserVersion returns the key for a user's monotonic version counter.
// Value: integer, INCR on every online/offline transition
// Hash tag: {scope_id:user_id} for user-scoped operations
func (k Key) UserVersion(scopeID, userID string) string {
	tag := k.hashTag(scopeID + ":" + userID)
	return fmt.Sprintf("presence:user_version:%s", tag)
}

// Scopes returns the key for the active scopes index set.
// Value: SET of scope_id
// No hash tag (global key)
func (k Key) Scopes() string {
	return "presence:scopes"
}

// DisconnectJobs returns the key for a scope's disconnect job queue.
// Value: ZSET (score=run_at_unix_ms, member=job_id)
// Hash tag: {scope_id} for scope-scoped operations
func (k Key) DisconnectJobs(scopeID string) string {
	tag := k.hashTag(scopeID)
	return fmt.Sprintf("presence:disconnect_jobs:%s", tag)
}

// DisconnectJobMeta returns the key for a disconnect job's metadata.
// Value: HASH {user_id, session_id, created_at}, TTL=60s
// Hash tag: {scope_id} for scope-scoped operations
func (k Key) DisconnectJobMeta(scopeID, jobID string) string {
	tag := k.hashTag(scopeID)
	return fmt.Sprintf("presence:disconnect_job_meta:%s:%s", tag, jobID)
}

// DisconnectJobIndex returns the key for the cancellation index.
// Value: SET of job_id for a specific user+session
// Hash tag: {scope_id:user_id} for user-scoped operations
func (k Key) DisconnectJobIndex(scopeID, userID, sessionID string) string {
	tag := k.hashTag(scopeID + ":" + userID)
	return fmt.Sprintf("presence:disconnect_job_index:%s:%s", tag, sessionID)
}

// SweepCursor returns the key for storing the sweep cursor for online_users.
// Value: string cursor value for SSCAN resume
// Hash tag: {scope_id} for scope-scoped operations
func (k Key) SweepCursor(scopeID string) string {
	tag := k.hashTag(scopeID)
	return fmt.Sprintf("presence:sweep_cursor:%s", tag)
}

// SweepUserCursor returns the key for storing per-user sweep cursor (optional).
// Value: string cursor value for SSCAN resume on user_sessions
// Hash tag: {scope_id:user_id} for user-scoped operations
func (k Key) SweepUserCursor(scopeID, userID string) string {
	tag := k.hashTag(scopeID + ":" + userID)
	return fmt.Sprintf("presence:sweep_user_cursor:%s", tag)
}

// PubSubChannel returns the Pub/Sub channel for a scope's presence events.
// No hash tag (Pub/Sub works differently in cluster mode)
func (k Key) PubSubChannel(scopeID string) string {
	return fmt.Sprintf("presence:scope:%s", scopeID)
}

// InvisibleUsers returns the key for tracking invisible users in a scope.
// Value: SET of user_id that are connected but invisible
// Hash tag: {scope_id} for scope-scoped operations
func (k Key) InvisibleUsers(scopeID string) string {
	tag := k.hashTag(scopeID)
	return fmt.Sprintf("presence:invisible_users:%s", tag)
}

// UserDevices returns the key for tracking device types for a user.
// Value: HASH {device_type -> session_count}
// Hash tag: {scope_id:user_id} for user-scoped operations
func (k Key) UserDevices(scopeID, userID string) string {
	tag := k.hashTag(scopeID + ":" + userID)
	return fmt.Sprintf("presence:user_devices:%s", tag)
}
