package rateschedule

import (
	"context"
	"fmt"
	"time"
)

const (
	ResultSuccess = "success"
	ResultError   = "error"
)

// Observation describes one rate schedule resolution.
type Observation struct {
	Type              string
	Result            string
	RequestsPerMinute int
	Duration          time.Duration
}

// Observer receives schedule measurements without being able to affect the
// workload.
type Observer interface {
	ObserveRateSchedule(ctx context.Context, observation Observation)
}

// Instrumented decorates a Schedule while preserving its values and errors.
type Instrumented struct {
	schedule     Schedule
	scheduleType string
	observer     Observer
	now          func() time.Time
}

var _ Schedule = (*Instrumented)(nil)

// NewInstrumented creates an observable rate schedule.
func NewInstrumented(schedule Schedule, scheduleType string, observer Observer) (*Instrumented, error) {
	if schedule == nil {
		return nil, fmt.Errorf("rate schedule must not be nil")
	}
	if scheduleType == "" {
		return nil, fmt.Errorf("rate schedule type must not be empty")
	}
	if observer == nil {
		return nil, fmt.Errorf("rate schedule observer must not be nil")
	}
	return &Instrumented{
		schedule:     schedule,
		scheduleType: scheduleType,
		observer:     observer,
		now:          time.Now,
	}, nil
}

func (s *Instrumented) RequestsPerMinute(ctx context.Context) (int, error) {
	started := s.now()
	requestsPerMinute, err := s.schedule.RequestsPerMinute(ctx)
	result := ResultSuccess
	if err != nil {
		result = ResultError
	}
	s.observer.ObserveRateSchedule(ctx, Observation{
		Type:              s.scheduleType,
		Result:            result,
		RequestsPerMinute: requestsPerMinute,
		Duration:          s.now().Sub(started),
	})
	return requestsPerMinute, err
}
