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
		decision, err := limiter.TryAcquire(context.Background(), limit, 1)
		if err != nil {
			t.Fatalf("acquisition %d error = %v", i+1, err)
		}
		if decision.Granted != 1 || decision.RetryAfter != 0 {
			t.Fatalf("acquisition %d decision = %#v; want allowed", i+1, decision)
		}
	}

	decision, err := limiter.TryAcquire(context.Background(), limit, 1)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Granted != 0 || decision.RetryAfter != 30*time.Second {
		t.Fatalf("rejected decision = %#v; want RetryAfter 30s", decision)
	}

	now = now.Add(time.Minute)
	decision, err = limiter.TryAcquire(context.Background(), limit, 1)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Granted != 1 {
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
			decision, err := limiter.TryAcquire(context.Background(), limit, 1)
			if err != nil {
				errorsChannel <- err
				return
			}
			allowed.Add(int32(decision.Granted))
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
	if _, err := limiter.TryAcquire(context.Background(), Limit{}, 1); err == nil {
		t.Fatal("invalid limit error = nil")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := limiter.TryAcquire(cancelled, Limit{Requests: 1, Window: time.Minute}, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquisition error = %v; want context.Canceled", err)
	}
}

func TestLocalLimiterPartiallyGrantsBatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 21, 12, 0, 30, 0, time.UTC)
	limiter := newLocalLimiter(func() time.Time { return now })
	limit := Limit{Requests: 5, Window: time.Minute}

	first, err := limiter.TryAcquire(context.Background(), limit, 3)
	if err != nil {
		t.Fatal(err)
	}
	if first.Granted != 3 || first.RetryAfter != 0 {
		t.Fatalf("first decision = %#v; want full grant", first)
	}

	second, err := limiter.TryAcquire(context.Background(), limit, 3)
	if err != nil {
		t.Fatal(err)
	}
	if second.Granted != 2 || second.RetryAfter != 30*time.Second {
		t.Fatalf("second decision = %#v; want partial grant with 30s retry", second)
	}
}
