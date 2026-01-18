-- Connect: Atomic session creation with online transition detection
-- KEYS[1] = presence:session:{scope_id}:{session_id}
-- KEYS[2] = presence:user_sessions:{scope_id}:{user_id}
-- KEYS[3] = presence:online_users:{scope_id}
-- KEYS[4] = presence:user_version:{scope_id}:{user_id}
-- KEYS[5] = presence:scopes
-- ARGV[1] = session metadata JSON (write-once)
-- ARGV[2] = TTL in seconds
-- ARGV[3] = session_id
-- ARGV[4] = user_id
-- ARGV[5] = scope_id
--
-- Returns: {transition, version, session_count}
--   transition: "online" if 0->1 transition, "none" otherwise
--   version: current user version (after potential increment)
--   session_count: current session count for the user

local session_key = KEYS[1]
local user_sessions_key = KEYS[2]
local online_users_key = KEYS[3]
local user_version_key = KEYS[4]
local scopes_key = KEYS[5]

local meta = ARGV[1]
local ttl = tonumber(ARGV[2])
local session_id = ARGV[3]
local user_id = ARGV[4]
local scope_id = ARGV[5]

-- Write session metadata (write-once, never updated after)
redis.call('SETEX', session_key, ttl, meta)

-- Add session to user's session set
local added = redis.call('SADD', user_sessions_key, session_id)

-- Check current session count
local session_count = redis.call('SCARD', user_sessions_key)

local transition = "none"
local version = redis.call('GET', user_version_key)
if version then
    version = tonumber(version)
else
    version = 0
end

-- Increment version on any new session
if added == 1 then
    version = redis.call('INCR', user_version_key)
end

if session_count == 1 then
    -- First session: user is coming online
    redis.call('SADD', online_users_key, user_id)
    redis.call('SADD', scopes_key, scope_id)
    transition = "online"
end

return {transition, version, session_count}

