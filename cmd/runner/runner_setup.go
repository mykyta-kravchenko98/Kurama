package main

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner/ratelimit"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner/rateschedule"
)

func instrumentRuntimeState(
	registerer prometheus.Registerer,
	state *runtimeState,
	storeBackend string,
	limiterBackend string,
	scheduleType string,
) error {
	if state == nil {
		return fmt.Errorf("runtime state must not be nil")
	}

	storeObserver, err := runner.NewPrometheusStoreObserver(registerer)
	if err != nil {
		return fmt.Errorf("create store metrics observer: %w", err)
	}
	instrumentedStore, err := runner.NewInstrumentedStore(state.ValueStore, storeBackend, storeObserver)
	if err != nil {
		return fmt.Errorf("instrument value store: %w", err)
	}
	state.ValueStore = instrumentedStore

	limiterObserver, err := ratelimit.NewPrometheusObserver(registerer)
	if err != nil {
		return fmt.Errorf("create rate limiter metrics observer: %w", err)
	}
	instrumentedLimiter, err := ratelimit.NewInstrumentedLimiter(state.Limiter, limiterBackend, limiterObserver)
	if err != nil {
		return fmt.Errorf("instrument rate limiter: %w", err)
	}
	state.Limiter = instrumentedLimiter

	scheduleObserver, err := rateschedule.NewPrometheusObserver(registerer)
	if err != nil {
		return fmt.Errorf("create rate schedule metrics observer: %w", err)
	}
	instrumentedSchedule, err := rateschedule.NewInstrumented(state.Schedule, scheduleType, scheduleObserver)
	if err != nil {
		return fmt.Errorf("instrument rate schedule: %w", err)
	}
	state.Schedule = instrumentedSchedule
	return nil
}

func newRunnerScheduler(
	config runner.Config,
	state *runtimeState,
	schedulerOptions ...runner.SchedulerOption,
) (*runner.Scheduler, error) {
	if state == nil {
		return nil, fmt.Errorf("runtime state must not be nil")
	}
	executor, err := runner.NewExecutor(config.Target, state)
	if err != nil {
		return nil, fmt.Errorf("create HTTP executor: %w", err)
	}
	options := []runner.SchedulerOption{
		runner.WithRateLimiter(state.Limiter),
		runner.WithRateSchedule(state.Schedule),
	}
	options = append(options, schedulerOptions...)
	scheduler, err := runner.NewScheduler(config.Rate, config.Operations, executor, options...)
	if err != nil {
		return nil, fmt.Errorf("create scheduler: %w", err)
	}
	return scheduler, nil
}
