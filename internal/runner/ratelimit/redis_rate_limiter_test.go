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
)

func TestRedisRateLimiterSharesBudgetBetweenInstances(t *testing.T) {
	t.Parallel()
	server, firstClient := newTestRedis(t)
	server.SetTime(time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC))
	secondClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		if err := secondClient.Close(); err != nil {
			t.Errorf("close second Redis client: %v", err)
		}
	})

	scope := RedisRateLimiterScope{Namespace: "shorturl", Scenario: "load"}
	first := newTestRedisRateLimiter(t, firstClient, scope)
	second := newTestRedisRateLimiter(t, secondClient, scope)
	limit := Limit{Requests: 3, Window: time.Minute}

	for i, limiter := range []*RedisRateLimiter{first, second, first} {
		decision, err := limiter.TryAcquire(context.Background(), limit)
		if err != nil {
			t.Fatalf("acquisition %d error = %v", i+1, err)
		}
		if !decision.Allowed || decision.RetryAfter != 0 {
			t.Fatalf("acquisition %d decision = %#v; want allowed", i+1, decision)
		}
	}

	decision, err := second.TryAcquire(context.Background(), limit)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed {
		t.Fatal("fourth acquisition was allowed for a three-request budget")
	}
	if decision.RetryAfter <= 0 || decision.RetryAfter > time.Minute {
		t.Fatalf("fourth acquisition RetryAfter = %s; want within one minute", decision.RetryAfter)
	}

	server.FastForward(time.Minute)
	decision, err = second.TryAcquire(context.Background(), limit)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed {
		t.Fatal("acquisition in a new window was rejected")
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

	scope := RedisRateLimiterScope{Namespace: "shorturl", Scenario: "load"}
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

func TestRedisRateLimiterKeepsScenariosIndependent(t *testing.T) {
	t.Parallel()
	_, client := newTestRedis(t)
	first := newTestRedisRateLimiter(t, client, RedisRateLimiterScope{Namespace: "shorturl", Scenario: "first"})
	second := newTestRedisRateLimiter(t, client, RedisRateLimiterScope{Namespace: "shorturl", Scenario: "second"})
	limit := Limit{Requests: 1, Window: time.Minute}

	for name, limiter := range map[string]*RedisRateLimiter{"first": first, "second": second} {
		decision, err := limiter.TryAcquire(context.Background(), limit)
		if err != nil {
			t.Fatalf("%s acquisition error = %v", name, err)
		}
		if !decision.Allowed {
			t.Fatalf("%s acquisition was rejected", name)
		}
	}
}

func TestRedisRateLimiterReportsValidationCancellationAndRedisErrors(t *testing.T) {
	t.Parallel()
	server, client := newTestRedis(t)
	limiter := newTestRedisRateLimiter(t, client, RedisRateLimiterScope{Namespace: "shorturl", Scenario: "load"})

	if _, err := limiter.TryAcquire(context.Background(), Limit{}); err == nil {
		t.Fatal("invalid limit error = nil")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := limiter.TryAcquire(cancelled, Limit{Requests: 1, Window: time.Minute}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquisition error = %v; want context.Canceled", err)
	}

	server.Close()
	if _, err := limiter.TryAcquire(context.Background(), Limit{Requests: 1, Window: time.Minute}); err == nil {
		t.Fatal("acquisition after Redis shutdown error = nil")
	}
}

func TestNewRedisRateLimiterValidatesConfiguration(t *testing.T) {
	t.Parallel()
	_, client := newTestRedis(t)
	validScope := RedisRateLimiterScope{Namespace: "shorturl", Scenario: "load"}
	tests := []struct {
		name   string
		client redis.UniversalClient
		scope  RedisRateLimiterScope
	}{
		{name: "nil client", scope: validScope},
		{name: "empty namespace", client: client, scope: RedisRateLimiterScope{Scenario: "load"}},
		{name: "empty scenario", client: client, scope: RedisRateLimiterScope{Namespace: "shorturl"}},
		{name: "namespace colon", client: client, scope: RedisRateLimiterScope{Namespace: "short:url", Scenario: "load"}},
		{name: "scenario colon", client: client, scope: RedisRateLimiterScope{Namespace: "shorturl", Scenario: "lo:ad"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewRedisRateLimiter(test.client, test.scope); err == nil {
				t.Fatal("NewRedisRateLimiter() error = nil")
			}
		})
	}
}

func newTestRedisRateLimiter(
	t *testing.T,
	client redis.UniversalClient,
	scope RedisRateLimiterScope,
) *RedisRateLimiter {
	t.Helper()
	limiter, err := NewRedisRateLimiter(client, scope)
	if err != nil {
		t.Fatalf("NewRedisRateLimiter() error = %v", err)
	}
	return limiter
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
