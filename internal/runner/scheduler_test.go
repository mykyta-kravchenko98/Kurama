package runner

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner/ratelimit"
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

func TestSchedulerExecutesOnlyWithRateLimitPermit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		decision       ratelimit.Decision
		limiterErr     error
		wantExecutions int
	}{
		{name: "allowed", decision: ratelimit.Decision{Allowed: true}, wantExecutions: 1},
		{name: "rejected"},
		{name: "limiter error", limiterErr: errors.New("redis unavailable")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			executor := &recordingExecutor{}
			limiter := &recordingRateLimiter{decision: test.decision, err: test.limiterErr}
			scheduler := newTestScheduler(t, executor, WithRateLimiter(limiter))

			scheduler.executeSlot(context.Background())
			if got := len(executor.operationNames()); got != test.wantExecutions {
				t.Fatalf("executions = %d; want %d", got, test.wantExecutions)
			}
			if limiter.calls != 1 {
				t.Fatalf("limiter calls = %d; want 1", limiter.calls)
			}
			wantLimit := ratelimit.Limit{Requests: 30, Window: time.Minute}
			if limiter.limit != wantLimit {
				t.Fatalf("limit = %#v; want %#v", limiter.limit, wantLimit)
			}
		})
	}
}

func TestSchedulerPassesCurrentScheduleRateToLimiter(t *testing.T) {
	t.Parallel()
	limiter := &recordingRateLimiter{decision: ratelimit.Decision{Allowed: true}}
	scheduler := newTestScheduler(t, &recordingExecutor{},
		WithRateLimiter(limiter),
		WithRateSchedule(fixedTestSchedule{requestsPerMinute: 45}),
	)

	scheduler.executeSlot(context.Background())
	want := ratelimit.Limit{Requests: 45, Window: time.Minute}
	if limiter.limit != want {
		t.Fatalf("limit = %#v; want %#v", limiter.limit, want)
	}
}

func TestSchedulerFailsClosedWhenRateScheduleFails(t *testing.T) {
	t.Parallel()
	executor := &recordingExecutor{}
	limiter := &recordingRateLimiter{decision: ratelimit.Decision{Allowed: true}}
	scheduler := newTestScheduler(t, executor,
		WithRateLimiter(limiter),
		WithRateSchedule(fixedTestSchedule{err: errors.New("redis unavailable")}),
	)

	scheduler.executeSlot(context.Background())
	if got := len(executor.operationNames()); got != 0 {
		t.Fatalf("executions = %d, want 0", got)
	}
	if limiter.calls != 0 {
		t.Fatalf("limiter calls = %d, want 0", limiter.calls)
	}
}

func TestNewSchedulerAcceptsExternalUniformSchedule(t *testing.T) {
	t.Parallel()
	rate := RateConfig{Schedule: RateScheduleConfig{
		Type:                 "uniform",
		MinRequestsPerMinute: 2,
		MaxRequestsPerMinute: 56,
		WindowMinutes:        1,
	}}
	_, err := NewScheduler(
		rate,
		schedulerOperations(),
		&recordingExecutor{},
		WithRateSchedule(fixedTestSchedule{requestsPerMinute: 45}),
	)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
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
		fixedRateConfig(45),
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

func TestSchedulerRunExecutesImmediatelyAndAfterProfileDelay(t *testing.T) {
	t.Parallel()
	executor := &recordingExecutor{called: make(chan struct{}, 2)}
	scheduler := newTestScheduler(t, executor,
		WithWeightedRandomSource(&sequenceRandomSource{values: []int{0, 0}}),
	)
	delays := make(chan time.Duration, 1)
	release := make(chan struct{}, 1)
	scheduler.wait = func(ctx context.Context, delay time.Duration) bool {
		delays <- delay
		select {
		case <-ctx.Done():
			return false
		case <-release:
			return true
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		scheduler.Run(ctx)
		close(done)
	}()
	waitForExecution(t, executor.called)
	select {
	case delay := <-delays:
		if delay != 2*time.Second {
			t.Fatalf("profile delay = %s; want 2s", delay)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not wait for profile delay")
	}
	release <- struct{}{}
	waitForExecution(t, executor.called)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after context cancellation")
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
		{name: "missing schedule", rate: RateConfig{}, operations: schedulerOperations(), executor: executor},
		{name: "no operations", rate: fixedRateConfig(30), executor: executor},
		{name: "zero weight", rate: fixedRateConfig(30), operations: []OperationConfig{{Name: "get"}}, executor: executor},
		{name: "nil executor", rate: fixedRateConfig(30), operations: schedulerOperations()},
		{name: "unknown profile", rate: RateConfig{
			Schedule: RateScheduleConfig{Type: "fixed", RequestsPerMinute: 30},
			Profile:  &RateProfileConfig{Type: "burst"},
		}, operations: schedulerOperations(), executor: executor},
		{name: "uniform without external implementation", rate: RateConfig{Schedule: RateScheduleConfig{
			Type: "uniform", MinRequestsPerMinute: 2, MaxRequestsPerMinute: 56, WindowMinutes: 1,
		}}, operations: schedulerOperations(), executor: executor},
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
		fixedRateConfig(30),
		schedulerOperations(),
		executor,
		options...,
	)
	if err != nil {
		t.Fatal(err)
	}
	return scheduler
}

func fixedRateConfig(requestsPerMinute int) RateConfig {
	return RateConfig{Schedule: RateScheduleConfig{Type: "fixed", RequestsPerMinute: requestsPerMinute}}
}

type sequenceRandomSource struct {
	values []int
	next   int
}

type recordingRateLimiter struct {
	decision ratelimit.Decision
	err      error
	limit    ratelimit.Limit
	calls    int
}

type fixedTestSchedule struct {
	requestsPerMinute int
	err               error
}

func (s fixedTestSchedule) RequestsPerMinute(context.Context) (int, error) {
	return s.requestsPerMinute, s.err
}

func (l *recordingRateLimiter) TryAcquire(_ context.Context, limit ratelimit.Limit) (ratelimit.Decision, error) {
	l.calls++
	l.limit = limit
	return l.decision, l.err
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

func waitForExecution(t *testing.T, calls <-chan struct{}) {
	t.Helper()
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for scheduler execution")
	}
}
