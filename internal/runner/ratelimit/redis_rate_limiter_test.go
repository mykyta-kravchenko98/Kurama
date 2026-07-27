package ratelimit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner/rediskey"
)

func TestRedisRateLimiterSharesBudgetBetweenInstances(t *testing.T) {
	t.Parallel()
	server, firstClient := newTestRedis(t)
	windowStart := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	server.SetTime(windowStart)
	secondClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		if err := secondClient.Close(); err != nil {
			t.Errorf("close second Redis client: %v", err)
		}
	})

	scope := newTestRedisRateLimiterScope(t, "scenario-uid")
	first := newTestRedisRateLimiter(t, firstClient, scope)
	second := newTestRedisRateLimiter(t, secondClient, scope)
	limit := Limit{Requests: 3, Window: time.Minute}

	for i, limiter := range []*RedisRateLimiter{first, second, first} {
		decision, err := limiter.TryAcquire(context.Background(), limit, 1)
		if err != nil {
			t.Fatalf("acquisition %d error = %v", i+1, err)
		}
		if decision.Granted != 1 || decision.RetryAfter != 0 {
			t.Fatalf("acquisition %d decision = %#v; want allowed", i+1, decision)
		}
	}

	decision, err := second.TryAcquire(context.Background(), limit, 1)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Granted != 0 {
		t.Fatal("fourth acquisition was allowed for a three-request budget")
	}
	if decision.RetryAfter <= 0 || decision.RetryAfter > time.Minute {
		t.Fatalf("fourth acquisition RetryAfter = %s; want within one minute", decision.RetryAfter)
	}

	// FastForward only advances miniredis TTLs; the TIME command used by the
	// Lua script follows the explicit server clock configured with SetTime.
	server.SetTime(windowStart.Add(time.Minute))
	decision, err = second.TryAcquire(context.Background(), limit, 1)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Granted != 1 {
		t.Fatal("acquisition in a new window was rejected")
	}
}

func TestRedisRateLimiterPartiallyGrantsBatchAcrossInstances(t *testing.T) {
	t.Parallel()
	server, firstClient := newTestRedis(t)
	server.SetTime(time.Date(2026, 7, 21, 12, 0, 30, 0, time.UTC))
	secondClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		if err := secondClient.Close(); err != nil {
			t.Errorf("close second Redis client: %v", err)
		}
	})

	scope := newTestRedisRateLimiterScope(t, "scenario-uid")
	first := newTestRedisRateLimiter(t, firstClient, scope)
	second := newTestRedisRateLimiter(t, secondClient, scope)
	limit := Limit{Requests: 5, Window: time.Minute}

	firstDecision, err := first.TryAcquire(context.Background(), limit, 3)
	if err != nil {
		t.Fatal(err)
	}
	if firstDecision.Granted != 3 || firstDecision.RetryAfter != 0 {
		t.Fatalf("first decision = %#v; want full batch", firstDecision)
	}

	secondDecision, err := second.TryAcquire(context.Background(), limit, 3)
	if err != nil {
		t.Fatal(err)
	}
	if secondDecision.Granted != 2 ||
		secondDecision.RetryAfter <= 0 || secondDecision.RetryAfter > 30*time.Second {
		t.Fatalf("second decision = %#v; want partial batch with retry", secondDecision)
	}

	rejected, err := first.TryAcquire(context.Background(), limit, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Granted != 0 || rejected.RetryAfter <= 0 {
		t.Fatalf("exhausted decision = %#v; want rejection", rejected)
	}
}

func TestRedisRateLimiterDoesNotExceedSharedBudgetConcurrently(t *testing.T) {
	t.Parallel()
	server, firstClient := newTestRedis(t)
	server.SetTime(time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC))
	secondClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		if err := secondClient.Close(); err != nil {
			t.Errorf("close second Redis client: %v", err)
		}
	})

	scope := newTestRedisRateLimiterScope(t, "scenario-uid")
	limiters := []*RedisRateLimiter{
		newTestRedisRateLimiter(t, firstClient, scope),
		newTestRedisRateLimiter(t, secondClient, scope),
	}
	limit := Limit{Requests: 40, Window: time.Minute}

	const attempts = 200
	var allowed atomic.Int32
	var wait sync.WaitGroup
	errorsChannel := make(chan error, attempts)
	for i := range attempts {
		limiter := limiters[i%len(limiters)]
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

func TestRedisRateLimiterKeepsScenariosIndependent(t *testing.T) {
	t.Parallel()
	_, client := newTestRedis(t)
	first := newTestRedisRateLimiter(t, client, newTestRedisRateLimiterScope(t, "first-uid"))
	second := newTestRedisRateLimiter(t, client, newTestRedisRateLimiterScope(t, "second-uid"))
	limit := Limit{Requests: 1, Window: time.Minute}

	for name, limiter := range map[string]*RedisRateLimiter{"first": first, "second": second} {
		decision, err := limiter.TryAcquire(context.Background(), limit, 1)
		if err != nil {
			t.Fatalf("%s acquisition error = %v", name, err)
		}
		if decision.Granted != 1 {
			t.Fatalf("%s acquisition was rejected", name)
		}
	}
}

func TestRedisRateLimiterReportsValidationCancellationAndRedisErrors(t *testing.T) {
	t.Parallel()
	server, client := newTestRedis(t)
	limiter := newTestRedisRateLimiter(t, client, newTestRedisRateLimiterScope(t, "scenario-uid"))

	if _, err := limiter.TryAcquire(context.Background(), Limit{}, 1); err == nil {
		t.Fatal("invalid limit error = nil")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := limiter.TryAcquire(cancelled, Limit{Requests: 1, Window: time.Minute}, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquisition error = %v; want context.Canceled", err)
	}

	server.Close()
	if _, err := limiter.TryAcquire(context.Background(), Limit{Requests: 1, Window: time.Minute}, 1); err == nil {
		t.Fatal("acquisition after Redis shutdown error = nil")
	}
}

func TestNewRedisRateLimiterValidatesConfiguration(t *testing.T) {
	t.Parallel()
	validScope := newTestRedisRateLimiterScope(t, "scenario-uid")
	if _, err := NewRedisRateLimiter(nil, validScope); err == nil {
		t.Fatal("NewRedisRateLimiter() error = nil")
	}
	_, client := newTestRedis(t)
	if _, err := NewRedisRateLimiter(client, rediskey.Scope{}); err == nil {
		t.Fatal("NewRedisRateLimiter() with invalid scope error = nil")
	}
}

func newTestRedisRateLimiter(
	t *testing.T,
	client redis.UniversalClient,
	scope rediskey.Scope,
) *RedisRateLimiter {
	t.Helper()
	limiter, err := NewRedisRateLimiter(client, scope)
	if err != nil {
		t.Fatalf("NewRedisRateLimiter() error = %v", err)
	}
	return limiter
}

func newTestRedisRateLimiterScope(t *testing.T, uid string) rediskey.Scope {
	t.Helper()
	scope, err := rediskey.NewScope("shorturl", "load", uid)
	if err != nil {
		t.Fatalf("rediskey.NewScope() error = %v", err)
	}
	return scope
}

func newTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close Redis client: %v", err)
		}
	})
	return server, client
}
