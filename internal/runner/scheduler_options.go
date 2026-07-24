package runner

import (
	"github.com/mykyta-kravchenko98/Kurama/internal/runner/ratelimit"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner/rateschedule"
)

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

func WithRateLimiter(limiter ratelimit.Limiter) SchedulerOption {
	return func(scheduler *Scheduler) {
		if limiter != nil {
			scheduler.limiter = limiter
		}
	}
}

func WithRateSchedule(schedule rateschedule.Schedule) SchedulerOption {
	return func(scheduler *Scheduler) {
		if schedule != nil {
			scheduler.schedule = schedule
		}
	}
}
