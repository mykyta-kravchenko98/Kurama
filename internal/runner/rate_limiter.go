package runner

import (
	"context"
	"fmt"
	"time"
)

// RateLimit describes a shared request budget. A successful acquisition
// consumes one of Requests permits in the current Window.
type RateLimit struct {
	Requests int
	Window   time.Duration
}

func (l RateLimit) Validate() error {
	if l.Requests < 1 {
		return fmt.Errorf("requests must be greater than zero")
	}
	if l.Window <= 0 {
		return fmt.Errorf("window must be greater than zero")
	}
	return nil
}

type RateLimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type RateLimiter interface {
	TryAcquire(ctx context.Context, limit RateLimit) (RateLimitDecision, error)
}
