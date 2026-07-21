package ratelimit

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisRateLimiterKeyPrefix = "kurama:v1:rate"

//go:embed acquire_rate_limit.lua
var acquireRateLimitLua string

var acquireRateLimitScript = redis.NewScript(acquireRateLimitLua)

// RedisRateLimiterScope isolates the shared request budget belonging to one
// TrafficScenario.
type RedisRateLimiterScope struct {
	Namespace string
	Scenario  string
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
	if err := validateRedisScope(scope.Namespace, scope.Scenario); err != nil {
		return nil, err
	}
	return &RedisRateLimiter{
		client: client,
		key: strings.Join([]string{
			redisRateLimiterKeyPrefix,
			scope.Namespace,
			scope.Scenario,
		}, ":"),
	}, nil
}

func validateRedisScope(namespace, scenario string) error {
	if namespace == "" {
		return fmt.Errorf("redis scope namespace must not be empty")
	}
	if scenario == "" {
		return fmt.Errorf("redis scope scenario must not be empty")
	}
	if strings.Contains(namespace, ":") {
		return fmt.Errorf("redis scope namespace must not contain colon")
	}
	if strings.Contains(scenario, ":") {
		return fmt.Errorf("redis scope scenario must not contain colon")
	}
	return nil
}

func (l *RedisRateLimiter) TryAcquire(ctx context.Context, limit Limit) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	if err := limit.Validate(); err != nil {
		return Decision{}, fmt.Errorf("validate rate limit: %w", err)
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
	).Int64Slice()
	if err != nil {
		return Decision{}, fmt.Errorf("acquire Redis rate limit: %w", err)
	}
	if len(result) != 2 {
		return Decision{}, fmt.Errorf("acquire Redis rate limit: unexpected result length %d", len(result))
	}

	return Decision{
		Allowed:    result[0] == 1,
		RetryAfter: time.Duration(result[1]) * time.Microsecond,
	}, nil
}
