local redis_time = redis.call("TIME")
local now_micros = tonumber(redis_time[1]) * 1000000 + tonumber(redis_time[2])
local requests = tonumber(ARGV[1])
local window_micros = tonumber(ARGV[2])
local requested_permits = tonumber(ARGV[3])
local window_start = now_micros - (now_micros % window_micros)
local window_token = string.format("%.0f", window_start)

local stored_window = redis.call("HGET", KEYS[1], "window")
local count = 0
if not stored_window or stored_window ~= window_token then
    redis.call("HSET", KEYS[1], "window", window_token, "count", count)
else
    count = tonumber(redis.call("HGET", KEYS[1], "count") or "0")
end

local remaining = math.max(requests - count, 0)
local granted = math.min(requested_permits, remaining)
if granted > 0 then
    redis.call("HINCRBY", KEYS[1], "count", granted)
end

local retry_after_micros = window_start + window_micros - now_micros
local ttl_millis = math.ceil((retry_after_micros + window_micros) / 1000)
redis.call("PEXPIRE", KEYS[1], ttl_millis)

if granted == requested_permits then
    return {granted, 0}
end
return {granted, retry_after_micros}
