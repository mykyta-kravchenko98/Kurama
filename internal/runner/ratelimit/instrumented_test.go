package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInstrumentedLimiterObservesResultsAndPreservesResponse(t *testing.T) {
	t.Parallel()
	limiterErr := errors.New("redis unavailable")
	tests := []struct {
		name       string
		decision   Decision
		err        error
		wantResult string
	}{
		{name: "allowed", decision: Decision{Allowed: true}, wantResult: ResultAllowed},
		{name: "rejected", decision: Decision{RetryAfter: time.Second}, wantResult: ResultRejected},
		{name: "error", err: limiterErr, wantResult: ResultError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			underlying := &stubLimiter{decision: test.decision, err: test.err}
			observer := &recordingObserver{}
			limiter, err := NewInstrumentedLimiter(underlying, "redis", observer)
			if err != nil {
				t.Fatal(err)
			}
			times := []time.Time{time.Unix(0, 0), time.Unix(0, int64(5*time.Millisecond))}
			limiter.now = func() time.Time {
				current := times[0]
				times = times[1:]
				return current
			}

			decision, acquireErr := limiter.TryAcquire(context.Background(), Limit{Requests: 1, Window: time.Minute})
			if decision != test.decision || !errors.Is(acquireErr, test.err) {
				t.Fatalf("TryAcquire() = (%#v, %v); want (%#v, %v)", decision, acquireErr, test.decision, test.err)
			}
			if len(observer.observations) != 1 {
				t.Fatalf("observations = %#v; want one", observer.observations)
			}
			want := Observation{Backend: "redis", Result: test.wantResult, Duration: 5 * time.Millisecond}
			if observer.observations[0] != want {
				t.Fatalf("observation = %#v; want %#v", observer.observations[0], want)
			}
		})
	}
}

func TestNewInstrumentedLimiterValidatesDependencies(t *testing.T) {
	t.Parallel()
	limiter := &stubLimiter{}
	observer := &recordingObserver{}
	tests := []struct {
		name     string
		limiter  Limiter
		backend  string
		observer Observer
	}{
		{name: "nil limiter", backend: "redis", observer: observer},
		{name: "empty backend", limiter: limiter, observer: observer},
		{name: "nil observer", limiter: limiter, backend: "redis"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewInstrumentedLimiter(test.limiter, test.backend, test.observer); err == nil {
				t.Fatal("NewInstrumentedLimiter() error = nil")
			}
		})
	}
}

type stubLimiter struct {
	decision Decision
	err      error
}

func (l *stubLimiter) TryAcquire(context.Context, Limit) (Decision, error) {
	return l.decision, l.err
}

type recordingObserver struct {
	observations []Observation
}

func (o *recordingObserver) ObserveRateLimit(_ context.Context, observation Observation) {
	o.observations = append(o.observations, observation)
}
