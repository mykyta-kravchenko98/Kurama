package ratelimit

import (
	"context"
	"fmt"
	"time"
)

// Limit describes a shared request budget. A successful acquisition
// consumes one of Requests permits in the current Window.
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

// Decision reports whether one request permit was acquired. A rejected
// acquisition may include the recommended delay before another attempt.
type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}

// Limiter atomically consumes request permits without waiting for one to
// become available.
type Limiter interface {
	TryAcquire(ctx context.Context, limit Limit) (Decision, error)
}
