package ratelimit

import (
	"context"
	"fmt"
	"time"
)

const (
	ResultAllowed  = "allowed"
	ResultRejected = "rejected"
	ResultError    = "error"
)

// Observation describes one limiter acquisition without exposing scenario
// values as metric labels.
type Observation struct {
	Backend  string
	Result   string
	Duration time.Duration
}

type Observer interface {
	ObserveRateLimit(ctx context.Context, observation Observation)
}

// InstrumentedLimiter decorates a Limiter while preserving its decisions and
// errors exactly.
type InstrumentedLimiter struct {
	limiter  Limiter
	backend  string
	observer Observer
	now      func() time.Time
}

var _ Limiter = (*InstrumentedLimiter)(nil)

func NewInstrumentedLimiter(limiter Limiter, backend string, observer Observer) (*InstrumentedLimiter, error) {
	if limiter == nil {
		return nil, fmt.Errorf("limiter must not be nil")
	}
	if backend == "" {
		return nil, fmt.Errorf("limiter backend must not be empty")
	}
	if observer == nil {
		return nil, fmt.Errorf("rate limit observer must not be nil")
	}
	return &InstrumentedLimiter{
		limiter:  limiter,
		backend:  backend,
		observer: observer,
		now:      time.Now,
	}, nil
}

func (l *InstrumentedLimiter) TryAcquire(ctx context.Context, limit Limit) (Decision, error) {
	started := l.now()
	decision, err := l.limiter.TryAcquire(ctx, limit)
	result := ResultRejected
	if err != nil {
		result = ResultError
	} else if decision.Allowed {
		result = ResultAllowed
	}
	l.observer.ObserveRateLimit(ctx, Observation{
		Backend:  l.backend,
		Result:   result,
		Duration: l.now().Sub(started),
	})
	return decision, err
}
