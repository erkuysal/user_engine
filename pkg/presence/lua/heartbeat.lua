-- Heartbeat: Extend session TTL (never rewrites value)
-- KEYS[1] = presence:session:{scope_id}:{session_id}
-- ARGV[1] = TTL in seconds
--
-- Returns: "OK" if session exists and was extended, "UNKNOWN" if session not found

local session_key = KEYS[1]
local ttl = tonumber(ARGV[1])

-- Check if session exists
local exists = redis.call('EXISTS', session_key)

if exists == 1 then
    -- Session exists: extend TTL only (never rewrite value)
    redis.call('EXPIRE', session_key, ttl)
    return "OK"
else
    -- Session not found (expired or never existed)
    return "UNKNOWN"
end

