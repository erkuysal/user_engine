-- Disconnect: Atomic session removal with offline transition detection
-- KEYS[1] = presence:session:{scope_id}:{session_id}
-- KEYS[2] = presence:user_sessions:{scope_id}:{user_id}
-- KEYS[3] = presence:online_users:{scope_id}
-- KEYS[4] = presence:user_version:{scope_id}:{user_id}
-- ARGV[1] = session_id
-- ARGV[2] = user_id
--
-- Returns: {transition, version, session_count}
--   transition: "offline" if 1->0 transition, "none" otherwise
--   version: current user version (after potential increment)
--   session_count: current session count for the user

local session_key = KEYS[1]
local user_sessions_key = KEYS[2]
local online_users_key = KEYS[3]
local user_version_key = KEYS[4]

local session_id = ARGV[1]
local user_id = ARGV[2]

-- Delete the session
redis.call('DEL', session_key)

-- Remove session from user's session set
local removed = redis.call('SREM', user_sessions_key, session_id)

-- Check if this was the last session (1->0 transition)
local session_count = redis.call('SCARD', user_sessions_key)

local transition = "none"
local version = redis.call('GET', user_version_key)
if version then
    version = tonumber(version)
else
    version = 0
end

if removed == 1 then
    version = redis.call('INCR', user_version_key)
end

if removed == 1 and session_count == 0 then
    -- Last session removed: user is going offline
    redis.call('SREM', online_users_key, user_id)
    transition = "offline"
end

return {transition, version, session_count}

