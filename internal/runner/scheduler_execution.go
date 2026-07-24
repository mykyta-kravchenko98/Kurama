package runner

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner/ratelimit"
)

func (s *Scheduler) executeSlot(ctx context.Context) {
	requestsPerMinute, ok := s.currentRequestsPerMinute(ctx)
	if !ok {
		return
	}
	decision, ok := s.reserveBatch(ctx, requestsPerMinute)
	if !ok {
		return
	}
	for range decision.Granted {
		s.executeReservedSlot(ctx)
	}
}

func (s *Scheduler) reserveBatch(ctx context.Context, requestsPerMinute int) (ratelimit.Decision, bool) {
	requestedPermits := s.profile.BatchSize(requestsPerMinute)
	if requestedPermits < 1 || requestedPermits > requestsPerMinute {
		slog.Error(
			"Kurama traffic profile returned invalid batch size",
			"batchSize", requestedPermits,
			"requestsPerMinute", requestsPerMinute,
		)
		return ratelimit.Decision{}, false
	}
	decision, err := s.limiter.TryAcquire(ctx, ratelimit.Limit{
		Requests: requestsPerMinute,
		Window:   time.Minute,
	}, requestedPermits)
	if err != nil {
		slog.Error("Kurama rate limiter failed", "error", err)
		return ratelimit.Decision{}, false
	}
	if decision.Granted < 0 || decision.Granted > requestedPermits {
		slog.Error(
			"Kurama rate limiter returned invalid permit count",
			"requestedPermits", requestedPermits,
			"grantedPermits", decision.Granted,
		)
		return ratelimit.Decision{}, false
	}
	if decision.Granted < requestedPermits {
		slog.Debug(
			"Kurama request batch was partially reserved",
			"requestedPermits", requestedPermits,
			"grantedPermits", decision.Granted,
			"retryAfter", decision.RetryAfter,
		)
	}
	return decision, true
}

func (s *Scheduler) executeReservedSlot(ctx context.Context) {
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

func (s *Scheduler) currentRequestsPerMinute(ctx context.Context) (int, bool) {
	requestsPerMinute, err := s.schedule.RequestsPerMinute(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Kurama rate schedule failed", "error", err)
		}
		return 0, false
	}
	if requestsPerMinute < 1 || requestsPerMinute > MaxRequestsPerMinute {
		slog.Error("Kurama rate schedule returned invalid RPM", "requestsPerMinute", requestsPerMinute)
		return 0, false
	}
	return requestsPerMinute, true
}
