local redis_time = redis.call("TIME")
local now_micros = tonumber(redis_time[1]) * 1000000 + tonumber(redis_time[2])
local window_micros = tonumber(ARGV[1])
local candidate = tonumber(ARGV[2])
local window_start = now_micros - (now_micros % window_micros)
local window_token = string.format("%.0f", window_start)

local stored_window = redis.call("HGET", KEYS[1], "window")
local selected
if not stored_window or stored_window ~= window_token then
    selected = candidate
    redis.call("HSET", KEYS[1], "window", window_token, "rpm", selected)
else
    selected = tonumber(redis.call("HGET", KEYS[1], "rpm"))
end

local remaining_micros = window_start + window_micros - now_micros
local ttl_millis = math.ceil((remaining_micros + window_micros) / 1000)
redis.call("PEXPIRE", KEYS[1], ttl_millis)

return selected
