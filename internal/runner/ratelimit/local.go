package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// LocalLimiter keeps a fixed-window request budget in one runner process. It
// is intended for single-replica scenarios that do not use Redis.
type LocalLimiter struct {
	mu          sync.Mutex
	now         func() time.Time
	windowStart time.Time
	window      time.Duration
	used        int
}

var _ Limiter = (*LocalLimiter)(nil)

func NewLocalLimiter() *LocalLimiter {
	return newLocalLimiter(time.Now)
}

func newLocalLimiter(now func() time.Time) *LocalLimiter {
	return &LocalLimiter{now: now}
}

func (l *LocalLimiter) TryAcquire(ctx context.Context, limit Limit) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	if err := limit.Validate(); err != nil {
		return Decision{}, fmt.Errorf("validate rate limit: %w", err)
	}

	now := l.now()
	windowStart := now.Truncate(limit.Window)

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.window != limit.Window || !l.windowStart.Equal(windowStart) {
		l.window = limit.Window
		l.windowStart = windowStart
		l.used = 0
	}
	if l.used >= limit.Requests {
		return Decision{
			RetryAfter: windowStart.Add(limit.Window).Sub(now),
		}, nil
	}

	l.used++
	return Decision{Allowed: true}, nil
}
