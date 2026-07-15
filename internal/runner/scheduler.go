package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"time"
)

// OperationExecutor is implemented by Executor and kept small so scheduler
// selection and timing can be tested without making HTTP requests.
type OperationExecutor interface {
	Execute(ctx context.Context, operation OperationConfig) (ExecutionResult, error)
}

type WeightedRandomSource interface {
	IntN(n int) int
}

type ExecutionHandler func(result ExecutionResult, err error)

type Scheduler struct {
	interval   time.Duration
	operations []OperationConfig
	executor   OperationExecutor
	random     WeightedRandomSource
	handle     ExecutionHandler
	newTicker  tickerFactory
}

type SchedulerOption func(*Scheduler)

func WithWeightedRandomSource(source WeightedRandomSource) SchedulerOption {
	return func(scheduler *Scheduler) {
		if source != nil {
			scheduler.random = source
		}
	}
}

func WithExecutionHandler(handler ExecutionHandler) SchedulerOption {
	return func(scheduler *Scheduler) {
		if handler != nil {
			scheduler.handle = handler
		}
	}
}

func NewScheduler(
	rate RateConfig,
	operations []OperationConfig,
	executor OperationExecutor,
	options ...SchedulerOption,
) (*Scheduler, error) {
	if rate.RequestsPerMinute < 1 || rate.RequestsPerMinute > MaxRequestsPerMinute {
		return nil, fmt.Errorf("rate.requestsPerMinute must be between 1 and %d", MaxRequestsPerMinute)
	}
	if len(operations) == 0 || len(operations) > MaxOperations {
		return nil, fmt.Errorf("operations must contain between 1 and %d entries", MaxOperations)
	}
	for i, operation := range operations {
		if operation.Weight < 1 || operation.Weight > 10_000 {
			return nil, fmt.Errorf("operations[%d].weight must be between 1 and 10000", i)
		}
	}
	if executor == nil {
		return nil, fmt.Errorf("operation executor must not be nil")
	}

	scheduler := &Scheduler{
		interval:   time.Minute / time.Duration(rate.RequestsPerMinute),
		operations: slices.Clone(operations),
		executor:   executor,
		random:     globalRandomSource{},
		handle:     logExecution,
		newTicker:  newRealTicker,
	}
	for _, option := range options {
		option(scheduler)
	}
	return scheduler, nil
}

// Run executes one operation immediately and then at the configured fixed-RPM
// interval. Calls are deliberately sequential: slow targets reduce achieved
// RPM instead of causing an unbounded queue or concurrent request burst.
func (s *Scheduler) Run(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	s.executeSlot(ctx)

	ticker := s.newTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			s.executeSlot(ctx)
		}
	}
}

func (s *Scheduler) executeSlot(ctx context.Context) {
	excluded := make([]bool, len(s.operations))
	for attempts := 0; attempts < len(s.operations); attempts++ {
		index, ok := pickWeighted(s.operations, excluded, s.random)
		if !ok {
			return
		}
		operation := s.operations[index]
		result, err := s.executor.Execute(ctx, operation)
		if result.Operation == "" {
			result.Operation = operation.Name
		}
		s.handle(result, err)
		if !errors.Is(err, ErrStoreValueUnavailable) {
			return
		}
		excluded[index] = true
	}
}

func pickWeighted(
	operations []OperationConfig,
	excluded []bool,
	random WeightedRandomSource,
) (int, bool) {
	totalWeight := 0
	for i, operation := range operations {
		if !excluded[i] {
			totalWeight += operation.Weight
		}
	}
	if totalWeight == 0 {
		return 0, false
	}

	selected := random.IntN(totalWeight)
	for i, operation := range operations {
		if excluded[i] {
			continue
		}
		if selected < operation.Weight {
			return i, true
		}
		selected -= operation.Weight
	}
	return 0, false
}

func logExecution(result ExecutionResult, err error) {
	attributes := []any{
		"operation", result.Operation,
		"status", result.StatusCode,
		"duration", result.Duration,
		"responseBytes", result.ResponseBytes,
		"captured", result.Captured,
	}
	if err == nil {
		slog.Info("Kurama request completed", attributes...)
		return
	}
	attributes = append(attributes, "error", err)
	if errors.Is(err, ErrStoreValueUnavailable) {
		slog.Warn("Kurama operation temporarily unavailable", attributes...)
		return
	}
	slog.Error("Kurama request failed", attributes...)
}

type globalRandomSource struct{}

func (globalRandomSource) IntN(n int) int {
	return rand.IntN(n)
}

type schedulerTicker interface {
	C() <-chan time.Time
	Stop()
}

type tickerFactory func(interval time.Duration) schedulerTicker

type realTicker struct {
	ticker *time.Ticker
}

func newRealTicker(interval time.Duration) schedulerTicker {
	return realTicker{ticker: time.NewTicker(interval)}
}

func (t realTicker) C() <-chan time.Time {
	return t.ticker.C
}

func (t realTicker) Stop() {
	t.ticker.Stop()
}
