package ratelimit

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner/rediskey"
)

//go:embed acquire_rate_limit.lua
var acquireRateLimitLua string

var acquireRateLimitScript = redis.NewScript(acquireRateLimitLua)

// RedisRateLimiterScope isolates the shared request budget belonging to one
// TrafficScenario.
type RedisRateLimiterScope struct {
	Namespace string
	Scenario  string
	UID       string
}

// RedisRateLimiter atomically shares one fixed-window request budget between
// all runner replicas of a TrafficScenario. Redis TIME defines window
// boundaries so Pod clock skew cannot create independent budgets.
type RedisRateLimiter struct {
	client redis.UniversalClient
	key    string
}

var _ Limiter = (*RedisRateLimiter)(nil)

func NewRedisRateLimiter(client redis.UniversalClient, scope RedisRateLimiterScope) (*RedisRateLimiter, error) {
	if client == nil {
		return nil, fmt.Errorf("redis client must not be nil")
	}
	keyScope, err := rediskey.NewScope(scope.Namespace, scope.Scenario, scope.UID)
	if err != nil {
		return nil, err
	}
	return &RedisRateLimiter{
		client: client,
		key:    keyScope.RateLimitKey(),
	}, nil
}

func (l *RedisRateLimiter) TryAcquire(ctx context.Context, limit Limit, permits int) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	if err := limit.Validate(); err != nil {
		return Decision{}, fmt.Errorf("validate rate limit: %w", err)
	}
	if err := validatePermits(limit, permits); err != nil {
		return Decision{}, fmt.Errorf("validate rate limit permits: %w", err)
	}
	windowMicros := limit.Window.Microseconds()
	if windowMicros < 1 {
		return Decision{}, fmt.Errorf("rate limit window must be at least one microsecond")
	}

	result, err := acquireRateLimitScript.Run(
		ctx,
		l.client,
		[]string{l.key},
		limit.Requests,
		windowMicros,
		permits,
	).Int64Slice()
	if err != nil {
		return Decision{}, fmt.Errorf("acquire Redis rate limit: %w", err)
	}
	if len(result) != 2 {
		return Decision{}, fmt.Errorf("acquire Redis rate limit: unexpected result length %d", len(result))
	}

	return Decision{
		Granted:    int(result[0]),
		RetryAfter: time.Duration(result[1]) * time.Microsecond,
	}, nil
}
