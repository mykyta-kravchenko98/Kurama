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
)

type storeSettings struct {
	Backend      string
	RedisAddress string
	Namespace    string
	Scenario     string
}

type runtimeState struct {
	runner.ValueStore
	Limiter  ratelimit.Limiter
	Schedule rateschedule.Schedule
	close    func() error
}

func (s *runtimeState) Close() error {
	return s.close()
}

func storeSettingsFromEnv() storeSettings {
	return storeSettings{
		Backend:      os.Getenv(runner.StoreBackendEnv),
		RedisAddress: os.Getenv(runner.RedisAddressEnv),
		Namespace:    os.Getenv(runner.NamespaceEnv),
		Scenario:     os.Getenv(runner.ScenarioEnv),
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

	var client *redis.Client
	closeState := func() error { return nil }
	if storeBackend == "redis" || limiterBackend == "redis" || scheduleConfig.Type == "uniform" {
		if settings.RedisAddress == "" {
			return nil, fmt.Errorf("%s must be set when Redis is used", runner.RedisAddressEnv)
		}
		if settings.Namespace == "" {
			return nil, fmt.Errorf("%s must be set when Redis is used", runner.NamespaceEnv)
		}
		if settings.Scenario == "" {
			return nil, fmt.Errorf("%s must be set when Redis is used", runner.ScenarioEnv)
		}
		client = redis.NewClient(&redis.Options{Addr: settings.RedisAddress})
		if err := client.Ping(ctx).Err(); err != nil {
			closeErr := client.Close()
			return nil, errors.Join(fmt.Errorf("ping Redis: %w", err), closeErr)
		}
		closeState = client.Close
	}

	var store runner.ValueStore
	var err error
	switch storeBackend {
	case "memory":
		store, err = runner.NewMemoryStore(configs)
	case "redis":
		store, err = runner.NewRedisStore(client, runner.RedisStoreScope{
			Namespace: settings.Namespace,
			Scenario:  settings.Scenario,
		}, configs)
	}
	if err != nil {
		return nil, errors.Join(err, closeState())
	}
	return newRuntimeStateWithComponents(store, client, closeState, settings, limiterBackend, scheduleConfig)
}

func newRuntimeStateWithComponents(
	store runner.ValueStore,
	client redis.UniversalClient,
	closeState func() error,
	settings storeSettings,
	limiterBackend string,
	scheduleConfig runner.RateScheduleConfig,
) (*runtimeState, error) {
	var limiter ratelimit.Limiter
	switch limiterBackend {
	case "local":
		limiter = ratelimit.NewLocalLimiter()
	case "redis":
		redisLimiter, err := ratelimit.NewRedisRateLimiter(client, ratelimit.RedisRateLimiterScope{
			Namespace: settings.Namespace,
			Scenario:  settings.Scenario,
		})
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
			rateschedule.RedisUniformScope{Namespace: settings.Namespace, Scenario: settings.Scenario},
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
	return &runtimeState{ValueStore: store, Limiter: limiter, Schedule: schedule, close: closeState}, nil
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
