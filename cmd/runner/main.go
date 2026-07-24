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
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
)

const (
	scenarioConfigPath     = "/etc/kurama/scenario.json"
	metricsShutdownTimeout = 5 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, scenarioConfigPath); err != nil {
		slog.Error("Kurama runner failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configPath string, schedulerOptions ...runner.SchedulerOption) (runErr error) {
	return runWithMetricsAddress(ctx, configPath, metricsAddressFromEnv(), schedulerOptions...)
}

func runWithMetricsAddress(
	ctx context.Context,
	configPath string,
	metricsAddress string,
	schedulerOptions ...runner.SchedulerOption,
) (runErr error) {
	config, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	settings := storeSettingsFromEnv()
	limiterBackend := normalizedRateLimiterBackend(config.Rate.Limiter, settings.Backend)
	state, err := newRuntimeState(ctx, settings, limiterBackend, config.Rate.Schedule, config.Stores)
	if err != nil {
		return fmt.Errorf("create runner state: %w", err)
	}
	defer func() {
		if err := state.Close(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close runner state: %w", err))
		}
	}()

	registry := prometheus.NewRegistry()
	if err := instrumentRuntimeState(
		registry,
		state,
		normalizedStoreBackend(settings.Backend),
		limiterBackend,
		config.Rate.Schedule.Type,
	); err != nil {
		return fmt.Errorf("instrument runner state: %w", err)
	}
	if err := runner.RegisterPrometheusRateProfileMetrics(registry, config.Rate.Profile); err != nil {
		return fmt.Errorf("register rate profile metrics: %w", err)
	}
	scheduler, err := newRunnerScheduler(config, state, schedulerOptions...)
	if err != nil {
		return err
	}
	metrics, err := startMetricsServer(metricsAddress, registry)
	if err != nil {
		return fmt.Errorf("start metrics server: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), metricsShutdownTimeout)
		defer cancel()
		if err := metrics.Shutdown(shutdownCtx); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("stop metrics server: %w", err))
		}
	}()

	slog.Info("Kurama runner ready",
		"config", configPath,
		"metricsAddress", metricsAddress,
		"storeBackend", normalizedStoreBackend(settings.Backend),
		"rateLimiterBackend", limiterBackend,
		"rateProfile", normalizedRateProfileType(config.Rate.Profile),
		"rateSchedule", config.Rate.Schedule.Type,
		"target", config.Target.BaseURL,
		"operations", len(config.Operations),
		"stores", len(config.Stores),
	)
	scheduler.Run(ctx)
	slog.Info("Kurama runner stopping")
	return nil
}
