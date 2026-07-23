// Command runner executes the HTTP workload for one TrafficScenario Pod.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner/ratelimit"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner/rateschedule"
)

const (
	scenarioConfigPath     = "/etc/kurama/scenario.json"
	defaultMetricsAddress  = ":8080"
	metricsShutdownTimeout = 5 * time.Second
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

type metricsServer struct {
	server  *http.Server
	address string
	done    <-chan error
}

func (s *runtimeState) Close() error {
	return s.close()
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
	observer, err := runner.NewPrometheusStoreObserver(registry)
	if err != nil {
		return fmt.Errorf("create store metrics observer: %w", err)
	}
	instrumentedStore, err := runner.NewInstrumentedStore(
		state.ValueStore,
		normalizedStoreBackend(settings.Backend),
		observer,
	)
	if err != nil {
		return fmt.Errorf("instrument value store: %w", err)
	}
	state.ValueStore = instrumentedStore
	limiterObserver, err := ratelimit.NewPrometheusObserver(registry)
	if err != nil {
		return fmt.Errorf("create rate limiter metrics observer: %w", err)
	}
	instrumentedLimiter, err := ratelimit.NewInstrumentedLimiter(
		state.Limiter,
		limiterBackend,
		limiterObserver,
	)
	if err != nil {
		return fmt.Errorf("instrument rate limiter: %w", err)
	}
	state.Limiter = instrumentedLimiter
	executor, err := runner.NewExecutor(config.Target, state)
	if err != nil {
		return fmt.Errorf("create HTTP executor: %w", err)
	}
	options := []runner.SchedulerOption{
		runner.WithRateLimiter(state.Limiter),
		runner.WithRateSchedule(state.Schedule),
	}
	options = append(options, schedulerOptions...)
	scheduler, err := runner.NewScheduler(config.Rate, config.Operations, executor, options...)
	if err != nil {
		return fmt.Errorf("create scheduler: %w", err)
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

func metricsAddressFromEnv() string {
	if address := os.Getenv(runner.MetricsAddrEnv); address != "" {
		return address
	}
	return defaultMetricsAddress
}

func startMetricsServer(address string, gatherer prometheus.Gatherer) (*metricsServer, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen on %q: %w", address, err)
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}))
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(listener)
	}()
	return &metricsServer{server: server, address: listener.Addr().String(), done: done}, nil
}

func (s *metricsServer) Shutdown(ctx context.Context) error {
	shutdownErr := s.server.Shutdown(ctx)
	var closeErr error
	if shutdownErr != nil {
		closeErr = s.server.Close()
	}
	serveErr := <-s.done
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(shutdownErr, closeErr, serveErr)
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
