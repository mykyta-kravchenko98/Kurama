package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner/ratelimit"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner/rateschedule"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner/rediskey"
)

type storeSettings struct {
	Backend      string
	RedisAddress string
	Namespace    string
	Scenario     string
	ScenarioUID  string
}

type runtimeState struct {
	runner.ValueStore
	Limiter  ratelimit.Limiter
	Schedule rateschedule.Schedule
	redis    redis.UniversalClient
	maintain func(context.Context) error
	close    func() error
}

func (s *runtimeState) Close() error {
	return s.close()
}

func (s *runtimeState) Ready(ctx context.Context) error {
	if s.redis == nil {
		return nil
	}
	if err := s.redis.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping Redis: %w", err)
	}
	return nil
}

func (s *runtimeState) Maintain(ctx context.Context) error {
	if s.maintain == nil {
		return nil
	}
	return s.maintain(ctx)
}

func storeSettingsFromEnv() storeSettings {
	return storeSettings{
		Backend:      os.Getenv(runner.StoreBackendEnv),
		RedisAddress: os.Getenv(runner.RedisAddressEnv),
		Namespace:    os.Getenv(runner.NamespaceEnv),
		Scenario:     os.Getenv(runner.ScenarioEnv),
		ScenarioUID:  os.Getenv(runner.ScenarioUIDEnv),
	}
}

func newRuntimeState(
	ctx context.Context,
	settings storeSettings,
	limiterBackend string,
	scheduleConfig runner.RateScheduleConfig,
	configs []runner.StoreConfig,
) (*runtimeState, error) {
	storeBackend := normalizedStoreBackend(settings.Backend)
	if storeBackend != "memory" && storeBackend != "redis" {
		return nil, fmt.Errorf("%s %q is unsupported; use memory or redis", runner.StoreBackendEnv, settings.Backend)
	}
	if limiterBackend != "local" && limiterBackend != "redis" {
		return nil, fmt.Errorf("rate limiter backend %q is unsupported; use local or redis", limiterBackend)
	}

	// Keep the optional client as a nil interface until Redis is actually
	// required. Assigning a nil *redis.Client to redis.UniversalClient would
	// produce a non-nil interface and make readiness call Ping on a nil pointer.
	var client redis.UniversalClient
	var keyScope rediskey.Scope
	closeState := func() error { return nil }
	if storeBackend == "redis" || limiterBackend == "redis" || scheduleConfig.Type == "uniform" {
		if settings.RedisAddress == "" {
			return nil, fmt.Errorf("%s must be set when Redis is used", runner.RedisAddressEnv)
		}
		var err error
		keyScope, err = rediskey.NewScope(settings.Namespace, settings.Scenario, settings.ScenarioUID)
		if err != nil {
			return nil, fmt.Errorf("validate Redis scope: %w", err)
		}
		client = runner.NewRedisClient(settings.RedisAddress)
		if err := client.Ping(ctx).Err(); err != nil {
			closeErr := client.Close()
			return nil, errors.Join(fmt.Errorf("ping Redis: %w", err), closeErr)
		}
		closeState = client.Close
	}

	var store runner.ValueStore
	var maintain func(context.Context) error
	var err error
	switch storeBackend {
	case "memory":
		store, err = runner.NewMemoryStore(configs)
	case "redis":
		var redisStore *runner.RedisStore
		redisStore, err = runner.NewRedisStore(client, keyScope, configs)
		if err == nil {
			store = redisStore
			maintain = redisStore.ReconcileKeys
		}
	}
	if err != nil {
		return nil, errors.Join(err, closeState())
	}
	state, err := newRuntimeStateWithComponents(
		store,
		client,
		closeState,
		keyScope,
		limiterBackend,
		scheduleConfig,
	)
	if err != nil {
		return nil, err
	}
	state.maintain = maintain
	if err := state.Maintain(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("initialize Redis store retention: %w", err), state.Close())
	}
	return state, nil
}

func newRuntimeStateWithComponents(
	store runner.ValueStore,
	client redis.UniversalClient,
	closeState func() error,
	keyScope rediskey.Scope,
	limiterBackend string,
	scheduleConfig runner.RateScheduleConfig,
) (*runtimeState, error) {
	var limiter ratelimit.Limiter
	switch limiterBackend {
	case "local":
		limiter = ratelimit.NewLocalLimiter()
	case "redis":
		redisLimiter, err := ratelimit.NewRedisRateLimiter(client, keyScope)
		if err != nil {
			return nil, errors.Join(err, closeState())
		}
		limiter = redisLimiter
	}

	var schedule rateschedule.Schedule
	switch scheduleConfig.Type {
	case "fixed":
		schedule = rateschedule.NewFixed(scheduleConfig.RequestsPerMinute)
	case "uniform":
		redisSchedule, err := rateschedule.NewRedisUniform(
			client,
			keyScope,
			rateschedule.RedisUniformConfig{
				MinRequestsPerMinute: scheduleConfig.MinRequestsPerMinute,
				MaxRequestsPerMinute: scheduleConfig.MaxRequestsPerMinute,
				Window:               time.Duration(scheduleConfig.WindowMinutes) * time.Minute,
			},
		)
		if err != nil {
			return nil, errors.Join(err, closeState())
		}
		schedule = redisSchedule
	default:
		return nil, errors.Join(fmt.Errorf("rate schedule type %q is unsupported", scheduleConfig.Type), closeState())
	}
	return &runtimeState{
		ValueStore: store,
		Limiter:    limiter,
		Schedule:   schedule,
		redis:      client,
		close:      closeState,
	}, nil
}

func normalizedStoreBackend(backend string) string {
	if backend == "" {
		return "memory"
	}
	return backend
}

func normalizedRateLimiterBackend(config *runner.RateLimiterConfig, storeBackend string) string {
	if config != nil && config.Type != "" {
		return config.Type
	}
	if normalizedStoreBackend(storeBackend) == "redis" {
		return "redis"
	}
	return "local"
}

func normalizedRateProfileType(config *runner.RateProfileConfig) string {
	if config == nil || config.Type == "" {
		return "fixed"
	}
	return config.Type
}
