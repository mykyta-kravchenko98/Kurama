// Command runner executes the HTTP workload for one TrafficScenario Pod.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
)

const scenarioConfigPath = "/etc/kurama/scenario.json"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, scenarioConfigPath); err != nil {
		slog.Error("Kurama runner failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configPath string, schedulerOptions ...runner.SchedulerOption) error {
	config, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	stores, err := runner.NewMemoryStore(config.Stores)
	if err != nil {
		return fmt.Errorf("create value store: %w", err)
	}
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
		"target", config.Target.BaseURL,
		"requestsPerMinute", config.Rate.RequestsPerMinute,
		"operations", len(config.Operations),
		"stores", len(config.Stores),
	)
	scheduler.Run(ctx)
	slog.Info("Kurama runner stopping")
	return nil
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
