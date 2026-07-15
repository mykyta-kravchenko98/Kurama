package runner

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPickWeightedUsesCumulativeWeightRanges(t *testing.T) {
	t.Parallel()
	operations := schedulerOperations()
	random := &sequenceRandomSource{values: []int{0, 19, 20, 89, 90, 99}}
	want := []string{"create", "create", "resolve-valid", "resolve-valid", "resolve-invalid", "resolve-invalid"}
	for i, wantName := range want {
		index, ok := pickWeighted(operations, make([]bool, len(operations)), random)
		if !ok || operations[index].Name != wantName {
			t.Fatalf("pick %d = %q, %v; want %q", i, operations[index].Name, ok, wantName)
		}
	}
}

func TestSchedulerReselectsWhenStoreOperationIsUnavailable(t *testing.T) {
	t.Parallel()
	executor := &recordingExecutor{
		execute: func(operation OperationConfig) error {
			if operation.Name == "resolve-valid" {
				return ErrStoreValueUnavailable
			}
			return nil
		},
	}
	scheduler := newTestScheduler(t, executor,
		WithWeightedRandomSource(&sequenceRandomSource{values: []int{20, 0}}),
	)

	scheduler.executeSlot(context.Background())
	if got, want := executor.operationNames(), []string{"resolve-valid", "create"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("executed operations = %v; want %v", got, want)
	}
}

func TestSchedulerDoesNotRetryOrdinaryExecutionError(t *testing.T) {
	t.Parallel()
	executor := &recordingExecutor{execute: func(OperationConfig) error { return errors.New("target failed") }}
	scheduler := newTestScheduler(t, executor,
		WithWeightedRandomSource(&sequenceRandomSource{values: []int{0}}),
	)

	scheduler.executeSlot(context.Background())
	if got := executor.operationNames(); len(got) != 1 || got[0] != "create" {
		t.Fatalf("executed operations = %v", got)
	}
}

func TestSchedulerStopsAfterAllOperationsAreUnavailable(t *testing.T) {
	t.Parallel()
	operations := []OperationConfig{
		{Name: "first", Weight: 1},
		{Name: "second", Weight: 1},
	}
	executor := &recordingExecutor{execute: func(OperationConfig) error { return ErrStoreValueUnavailable }}
	scheduler, err := NewScheduler(
		RateConfig{RequestsPerMinute: 30},
		operations,
		executor,
		WithWeightedRandomSource(&sequenceRandomSource{values: []int{0, 0}}),
		WithExecutionHandler(func(ExecutionResult, error) {}),
	)
	if err != nil {
		t.Fatal(err)
	}

	scheduler.executeSlot(context.Background())
	if got, want := executor.operationNames(), []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("executed operations = %v; want %v", got, want)
	}
}

func TestSchedulerRunExecutesImmediatelyAndOnTicks(t *testing.T) {
	t.Parallel()
	executor := &recordingExecutor{called: make(chan struct{}, 2)}
	scheduler := newTestScheduler(t, executor,
		WithWeightedRandomSource(&sequenceRandomSource{values: []int{0, 0}}),
	)
	ticker := &manualTicker{ticks: make(chan time.Time, 1)}
	scheduler.newTicker = func(interval time.Duration) schedulerTicker {
		if interval != 2*time.Second {
			t.Errorf("ticker interval = %s; want 2s", interval)
		}
		return ticker
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		scheduler.Run(ctx)
		close(done)
	}()
	waitForExecution(t, executor.called)
	ticker.ticks <- time.Now()
	waitForExecution(t, executor.called)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after context cancellation")
	}
	if !ticker.stopped.Load() {
		t.Fatal("ticker was not stopped")
	}
}

func TestNewSchedulerValidatesInputs(t *testing.T) {
	t.Parallel()
	executor := &recordingExecutor{}
	tests := []struct {
		name       string
		rate       RateConfig
		operations []OperationConfig
		executor   OperationExecutor
	}{
		{name: "zero RPM", rate: RateConfig{}, operations: schedulerOperations(), executor: executor},
		{name: "no operations", rate: RateConfig{RequestsPerMinute: 30}, executor: executor},
		{name: "zero weight", rate: RateConfig{RequestsPerMinute: 30}, operations: []OperationConfig{{Name: "get"}}, executor: executor},
		{name: "nil executor", rate: RateConfig{RequestsPerMinute: 30}, operations: schedulerOperations()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewScheduler(test.rate, test.operations, test.executor); err == nil {
				t.Fatal("NewScheduler() error = nil")
			}
		})
	}
}

func schedulerOperations() []OperationConfig {
	return []OperationConfig{
		{Name: "create", Weight: 20},
		{Name: "resolve-valid", Weight: 70},
		{Name: "resolve-invalid", Weight: 10},
	}
}

func newTestScheduler(t *testing.T, executor OperationExecutor, options ...SchedulerOption) *Scheduler {
	t.Helper()
	options = append(options, WithExecutionHandler(func(ExecutionResult, error) {}))
	scheduler, err := NewScheduler(
		RateConfig{RequestsPerMinute: 30},
		schedulerOperations(),
		executor,
		options...,
	)
	if err != nil {
		t.Fatal(err)
	}
	return scheduler
}

type sequenceRandomSource struct {
	values []int
	next   int
}

func (s *sequenceRandomSource) IntN(n int) int {
	value := s.values[s.next]
	s.next++
	if value < 0 || value >= n {
		panic("test random value is outside IntN range")
	}
	return value
}

type recordingExecutor struct {
	mu      sync.Mutex
	names   []string
	execute func(OperationConfig) error
	called  chan struct{}
}

func (e *recordingExecutor) Execute(_ context.Context, operation OperationConfig) (ExecutionResult, error) {
	e.mu.Lock()
	e.names = append(e.names, operation.Name)
	e.mu.Unlock()
	if e.called != nil {
		e.called <- struct{}{}
	}
	var err error
	if e.execute != nil {
		err = e.execute(operation)
	}
	return ExecutionResult{Operation: operation.Name}, err
}

func (e *recordingExecutor) operationNames() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.names...)
}

type manualTicker struct {
	ticks   chan time.Time
	stopped atomic.Bool
}

func (t *manualTicker) C() <-chan time.Time {
	return t.ticks
}

func (t *manualTicker) Stop() {
	t.stopped.Store(true)
}

func waitForExecution(t *testing.T, calls <-chan struct{}) {
	t.Helper()
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for scheduler execution")
	}
}
