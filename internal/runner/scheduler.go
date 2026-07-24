package runner

import (
	"context"
	"fmt"
	"slices"
	"time"

	trafficprofile "github.com/mykyta-kravchenko98/Kurama/internal/runner/profile"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner/ratelimit"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner/rateschedule"
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
	profile    trafficprofile.Timing
	operations []OperationConfig
	executor   OperationExecutor
	random     WeightedRandomSource
	handle     ExecutionHandler
	wait       waitForDelay
	limiter    ratelimit.Limiter
	schedule   rateschedule.Schedule
}

func NewScheduler(
	rate RateConfig,
	operations []OperationConfig,
	executor OperationExecutor,
	options ...SchedulerOption,
) (*Scheduler, error) {
	if err := validateRateSchedule(rate.Schedule); err != nil {
		return nil, err
	}
	if err := validateRateProfile(rate.Profile); err != nil {
		return nil, err
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
	profileConfig := trafficprofile.Config{}
	if rate.Profile != nil {
		profileConfig = trafficprofile.Config{
			Type:         rate.Profile.Type,
			MinBurstSize: rate.Profile.MinBurstSize,
			MaxBurstSize: rate.Profile.MaxBurstSize,
			DelayDivisor: rate.Profile.DelayDivisor,
		}
	}
	delayProfile, err := trafficprofile.New(profileConfig)
	if err != nil {
		return nil, fmt.Errorf("create traffic profile: %w", err)
	}
	var defaultSchedule rateschedule.Schedule
	if rate.Schedule.Type == "fixed" {
		defaultSchedule = rateschedule.NewFixed(rate.Schedule.RequestsPerMinute)
	}

	scheduler := &Scheduler{
		profile:    delayProfile,
		operations: slices.Clone(operations),
		executor:   executor,
		random:     globalRandomSource{},
		handle:     logExecution,
		wait:       waitWithTimer,
		limiter:    ratelimit.NewLocalLimiter(),
		schedule:   defaultSchedule,
	}
	for _, option := range options {
		option(scheduler)
	}
	if scheduler.schedule == nil {
		return nil, fmt.Errorf("rate.schedule %q requires an external schedule implementation", rate.Schedule.Type)
	}
	return scheduler, nil
}

// Run atomically reserves the next profile batch and executes its operations
// sequentially. Slow targets reduce achieved RPM instead of creating an
// unbounded request queue. A runner crash may leave reserved permits unused
// until the current limiter window expires.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		requestsPerMinute, ok := s.currentRequestsPerMinute(ctx)
		if !ok {
			if ctx.Err() != nil {
				return
			}
			if !s.wait(ctx, time.Second) {
				return
			}
			continue
		}
		decision, ok := s.reserveBatch(ctx, requestsPerMinute)
		if !ok {
			if !s.wait(ctx, time.Second) {
				return
			}
			continue
		}
		if decision.Granted == 0 {
			delay := decision.RetryAfter
			if delay <= 0 {
				delay = time.Second
			}
			if !s.wait(ctx, delay) {
				return
			}
			continue
		}
		for position := 1; position <= decision.Granted; position++ {
			s.executeReservedSlot(ctx)
			delay := s.profile.NextDelay(trafficprofile.DelayContext{
				RequestsPerMinute: requestsPerMinute,
				Position:          position,
				BatchSize:         decision.Granted,
			})
			if position == decision.Granted && decision.RetryAfter > delay {
				delay = decision.RetryAfter
			}
			if !s.wait(ctx, delay) {
				return
			}
		}
	}
}
