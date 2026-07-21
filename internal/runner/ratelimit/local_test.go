package ratelimit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLocalLimiterEnforcesBudgetAndResetsInNewWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 21, 12, 0, 30, 0, time.UTC)
	limiter := newLocalLimiter(func() time.Time { return now })
	limit := Limit{Requests: 2, Window: time.Minute}

	for i := 0; i < limit.Requests; i++ {
		decision, err := limiter.TryAcquire(context.Background(), limit)
		if err != nil {
			t.Fatalf("acquisition %d error = %v", i+1, err)
		}
		if !decision.Allowed || decision.RetryAfter != 0 {
			t.Fatalf("acquisition %d decision = %#v; want allowed", i+1, decision)
		}
	}

	decision, err := limiter.TryAcquire(context.Background(), limit)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed || decision.RetryAfter != 30*time.Second {
		t.Fatalf("rejected decision = %#v; want RetryAfter 30s", decision)
	}

	now = now.Add(time.Minute)
	decision, err = limiter.TryAcquire(context.Background(), limit)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed {
		t.Fatal("acquisition in a new window was rejected")
	}
}

func TestLocalLimiterDoesNotExceedBudgetConcurrently(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	limiter := newLocalLimiter(func() time.Time { return fixedNow })
	limit := Limit{Requests: 25, Window: time.Minute}

	var allowed atomic.Int32
	var wait sync.WaitGroup
	errorsChannel := make(chan error, 100)
	for range 100 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			decision, err := limiter.TryAcquire(context.Background(), limit)
			if err != nil {
				errorsChannel <- err
				return
			}
			if decision.Allowed {
				allowed.Add(1)
			}
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Errorf("TryAcquire() error = %v", err)
	}
	if got := allowed.Load(); got != int32(limit.Requests) {
		t.Fatalf("allowed acquisitions = %d; want %d", got, limit.Requests)
	}
}

func TestLocalLimiterReportsValidationAndCancellationErrors(t *testing.T) {
	t.Parallel()
	limiter := NewLocalLimiter()
	if _, err := limiter.TryAcquire(context.Background(), Limit{}); err == nil {
		t.Fatal("invalid limit error = nil")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := limiter.TryAcquire(cancelled, Limit{Requests: 1, Window: time.Minute}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquisition error = %v; want context.Canceled", err)
	}
}
