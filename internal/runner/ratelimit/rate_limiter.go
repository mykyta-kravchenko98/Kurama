package ratelimit

import (
	"context"
	"fmt"
	"time"
)

// Limit describes a shared request budget.
type Limit struct {
	Requests int
	Window   time.Duration
}

func (l Limit) Validate() error {
	if l.Requests < 1 {
		return fmt.Errorf("requests must be greater than zero")
	}
	if l.Window <= 0 {
		return fmt.Errorf("window must be greater than zero")
	}
	return nil
}

func validatePermits(limit Limit, permits int) error {
	if permits < 1 {
		return fmt.Errorf("permits must be greater than zero")
	}
	if permits > limit.Requests {
		return fmt.Errorf("permits must not exceed the request budget")
	}
	return nil
}

// Decision reports how many request permits were atomically reserved. A
// partial or rejected acquisition includes the delay until the next window.
type Decision struct {
	Granted    int
	RetryAfter time.Duration
}

// Limiter atomically reserves up to permits request slots without waiting.
type Limiter interface {
	TryAcquire(ctx context.Context, limit Limit, permits int) (Decision, error)
}
