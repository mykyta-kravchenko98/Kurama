package main

import (
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	redisDialTimeout  = 3 * time.Second
	redisReadTimeout  = 2 * time.Second
	redisWriteTimeout = 2 * time.Second
	redisMaxRetries   = 2
)

func newRedisClient(address string) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:                  address,
		DialTimeout:           redisDialTimeout,
		ReadTimeout:           redisReadTimeout,
		WriteTimeout:          redisWriteTimeout,
		MaxRetries:            redisMaxRetries,
		DialerRetries:         redisMaxRetries,
		ContextTimeoutEnabled: true,
	})
}
