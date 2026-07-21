local redis_time = redis.call("TIME")
local now_micros = tonumber(redis_time[1]) * 1000000 + tonumber(redis_time[2])
local requests = tonumber(ARGV[1])
local window_micros = tonumber(ARGV[2])
local window_start = now_micros - (now_micros % window_micros)
local window_token = string.format("%.0f", window_start)

local stored_window = redis.call("HGET", KEYS[1], "window")
local count
if not stored_window or stored_window ~= window_token then
    count = 1
    redis.call("HSET", KEYS[1], "window", window_token, "count", count)
else
    count = redis.call("HINCRBY", KEYS[1], "count", 1)
end

local retry_after_micros = window_start + window_micros - now_micros
local ttl_millis = math.ceil((retry_after_micros + window_micros) / 1000)
redis.call("PEXPIRE", KEYS[1], ttl_millis)

if count <= requests then
    return {1, 0}
end
return {0, retry_after_micros}
