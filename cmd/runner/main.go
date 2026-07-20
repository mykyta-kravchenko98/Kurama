// Command runner executes the HTTP workload for one TrafficScenario Pod.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/redis/go-redis/v9"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
)

const scenarioConfigPath = "/etc/kurama/scenario.json"

type storeSettings struct {
	Backend      string
	RedisAddress string
	Namespace    string
	Scenario     string
}

type valueStoreHandle struct {
	runner.ValueStore
	close func() error
}

func (h *valueStoreHandle) Close() error {
	return h.close()
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, scenarioConfigPath); err != nil {
		slog.Error("Kurama runner failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configPath string, schedulerOptions ...runner.SchedulerOption) (runErr error) {
	config, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	settings := storeSettingsFromEnv()
	stores, err := newValueStore(ctx, settings, config.Stores)
	if err != nil {
		return fmt.Errorf("create value store: %w", err)
	}
	defer func() {
		if err := stores.Close(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close value store: %w", err))
		}
	}()
	executor, err := runner.NewExecutor(config.Target, stores)
	if err != nil {
		return fmt.Errorf("create HTTP executor: %w", err)
	}
	scheduler, err := runner.NewScheduler(config.Rate, config.Operations, executor, schedulerOptions...)
	if err != nil {
		return fmt.Errorf("create scheduler: %w", err)
	}

	slog.Info("Kurama runner ready",
		"config", configPath,
		"storeBackend", normalizedStoreBackend(settings.Backend),
		"target", config.Target.BaseURL,
		"requestsPerMinute", config.Rate.RequestsPerMinute,
		"operations", len(config.Operations),
		"stores", len(config.Stores),
	)
	scheduler.Run(ctx)
	slog.Info("Kurama runner stopping")
	return nil
}

func storeSettingsFromEnv() storeSettings {
	return storeSettings{
		Backend:      os.Getenv(runner.StoreBackendEnv),
		RedisAddress: os.Getenv(runner.RedisAddressEnv),
		Namespace:    os.Getenv(runner.NamespaceEnv),
		Scenario:     os.Getenv(runner.ScenarioEnv),
	}
}

func newValueStore(ctx context.Context, settings storeSettings, configs []runner.StoreConfig) (*valueStoreHandle, error) {
	switch normalizedStoreBackend(settings.Backend) {
	case "memory":
		store, err := runner.NewMemoryStore(configs)
		if err != nil {
			return nil, err
		}
		return &valueStoreHandle{ValueStore: store, close: func() error { return nil }}, nil
	case "redis":
		if settings.RedisAddress == "" {
			return nil, fmt.Errorf("%s must be set for Redis storage", runner.RedisAddressEnv)
		}
		if settings.Namespace == "" {
			return nil, fmt.Errorf("%s must be set for Redis storage", runner.NamespaceEnv)
		}
		if settings.Scenario == "" {
			return nil, fmt.Errorf("%s must be set for Redis storage", runner.ScenarioEnv)
		}
		client := redis.NewClient(&redis.Options{Addr: settings.RedisAddress})
		if err := client.Ping(ctx).Err(); err != nil {
			closeErr := client.Close()
			return nil, errors.Join(fmt.Errorf("ping Redis: %w", err), closeErr)
		}
		store, err := runner.NewRedisStore(client, runner.RedisStoreScope{
			Namespace: settings.Namespace,
			Scenario:  settings.Scenario,
		}, configs)
		if err != nil {
			closeErr := client.Close()
			return nil, errors.Join(err, closeErr)
		}
		return &valueStoreHandle{ValueStore: store, close: client.Close}, nil
	default:
		return nil, fmt.Errorf("%s %q is unsupported; use memory or redis", runner.StoreBackendEnv, settings.Backend)
	}
}

func normalizedStoreBackend(backend string) string {
	if backend == "" {
		return "memory"
	}
	return backend
}

func loadConfig(path string) (runner.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return runner.Config{}, fmt.Errorf("open runner config %q: %w", path, err)
	}
	config, decodeErr := runner.DecodeConfig(file)
	closeErr := file.Close()
	if decodeErr != nil {
		return runner.Config{}, fmt.Errorf("load runner config %q: %w", path, decodeErr)
	}
	if closeErr != nil {
		return runner.Config{}, fmt.Errorf("close runner config %q: %w", path, closeErr)
	}
	return config, nil
}
