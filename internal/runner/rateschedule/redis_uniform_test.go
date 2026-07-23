package rateschedule

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisUniformSharesFirstProposalAndChangesAtNextWindow(t *testing.T) {
	t.Parallel()
	server, firstClient := newTestRedis(t)
	windowStart := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	server.SetTime(windowStart)
	secondClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		if err := secondClient.Close(); err != nil {
			t.Errorf("close second Redis client: %v", err)
		}
	})

	scope := RedisUniformScope{Namespace: "shorturl", Scenario: "load"}
	config := RedisUniformConfig{MinRequestsPerMinute: 2, MaxRequestsPerMinute: 56, Window: time.Minute}
	first := newTestRedisUniform(t, firstClient, scope, config, fixedIntegerRandomSource{value: 0})
	second := newTestRedisUniform(t, secondClient, scope, config, fixedIntegerRandomSource{value: 54})

	firstRPM, err := first.RequestsPerMinute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	secondRPM, err := second.RequestsPerMinute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if firstRPM != 2 || secondRPM != firstRPM {
		t.Fatalf("first window RPMs = (%d, %d), want (2, 2)", firstRPM, secondRPM)
	}

	server.SetTime(windowStart.Add(time.Minute))
	nextRPM, err := second.RequestsPerMinute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if nextRPM != 56 {
		t.Fatalf("next window RPM = %d, want 56", nextRPM)
	}
}

func TestRedisUniformSelectsOneProposalConcurrently(t *testing.T) {
	t.Parallel()
	server, firstClient := newTestRedis(t)
	server.SetTime(time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC))
	secondClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		if err := secondClient.Close(); err != nil {
			t.Errorf("close second Redis client: %v", err)
		}
	})

	scope := RedisUniformScope{Namespace: "shorturl", Scenario: "load"}
	config := RedisUniformConfig{MinRequestsPerMinute: 2, MaxRequestsPerMinute: 56, Window: time.Minute}
	schedules := []*RedisUniform{
		newTestRedisUniform(t, firstClient, scope, config, fixedIntegerRandomSource{value: 0}),
		newTestRedisUniform(t, secondClient, scope, config, fixedIntegerRandomSource{value: 54}),
	}

	results := make([]int, len(schedules))
	errorsChannel := make(chan error, len(schedules))
	var wait sync.WaitGroup
	for index, schedule := range schedules {
		wait.Add(1)
		go func() {
			defer wait.Done()
			value, err := schedule.RequestsPerMinute(context.Background())
			if err != nil {
				errorsChannel <- err
				return
			}
			results[index] = value
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Errorf("RequestsPerMinute() error = %v", err)
	}
	if results[0] != results[1] || (results[0] != 2 && results[0] != 56) {
		t.Fatalf("concurrent RPMs = %v, want both 2 or both 56", results)
	}
}

func TestRedisUniformKeepsScopesIndependent(t *testing.T) {
	t.Parallel()
	_, client := newTestRedis(t)
	config := RedisUniformConfig{MinRequestsPerMinute: 2, MaxRequestsPerMinute: 56, Window: time.Minute}
	first := newTestRedisUniform(t, client,
		RedisUniformScope{Namespace: "shorturl", Scenario: "first"},
		config, fixedIntegerRandomSource{value: 0})
	second := newTestRedisUniform(t, client,
		RedisUniformScope{Namespace: "shorturl", Scenario: "second"},
		config, fixedIntegerRandomSource{value: 54})

	firstRPM, err := first.RequestsPerMinute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	secondRPM, err := second.RequestsPerMinute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if firstRPM != 2 || secondRPM != 56 {
		t.Fatalf("independent RPMs = (%d, %d), want (2, 56)", firstRPM, secondRPM)
	}
}

func TestRedisUniformReportsCancellationAndRedisErrors(t *testing.T) {
	t.Parallel()
	server, client := newTestRedis(t)
	schedule := newTestRedisUniform(t, client,
		RedisUniformScope{Namespace: "shorturl", Scenario: "load"},
		RedisUniformConfig{MinRequestsPerMinute: 2, MaxRequestsPerMinute: 56, Window: time.Minute},
		fixedIntegerRandomSource{value: 0})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := schedule.RequestsPerMinute(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request error = %v, want context.Canceled", err)
	}
	server.Close()
	if _, err := schedule.RequestsPerMinute(context.Background()); err == nil {
		t.Fatal("request after Redis shutdown error = nil")
	}
}

func TestNewRedisUniformValidatesConfiguration(t *testing.T) {
	t.Parallel()
	_, client := newTestRedis(t)
	validScope := RedisUniformScope{Namespace: "shorturl", Scenario: "load"}
	validConfig := RedisUniformConfig{MinRequestsPerMinute: 2, MaxRequestsPerMinute: 56, Window: time.Minute}
	tests := []struct {
		name   string
		client redis.UniversalClient
		scope  RedisUniformScope
		config RedisUniformConfig
		random integerRandomSource
	}{
		{name: "nil client", scope: validScope, config: validConfig, random: fixedIntegerRandomSource{}},
		{name: "empty namespace", client: client, scope: RedisUniformScope{Scenario: "load"}, config: validConfig, random: fixedIntegerRandomSource{}},
		{name: "empty scenario", client: client, scope: RedisUniformScope{Namespace: "shorturl"}, config: validConfig, random: fixedIntegerRandomSource{}},
		{name: "namespace colon", client: client, scope: RedisUniformScope{Namespace: "short:url", Scenario: "load"}, config: validConfig, random: fixedIntegerRandomSource{}},
		{name: "scenario colon", client: client, scope: RedisUniformScope{Namespace: "shorturl", Scenario: "lo:ad"}, config: validConfig, random: fixedIntegerRandomSource{}},
		{name: "zero minimum", client: client, scope: validScope, config: RedisUniformConfig{MaxRequestsPerMinute: 56, Window: time.Minute}, random: fixedIntegerRandomSource{}},
		{name: "maximum below minimum", client: client, scope: validScope, config: RedisUniformConfig{MinRequestsPerMinute: 56, MaxRequestsPerMinute: 2, Window: time.Minute}, random: fixedIntegerRandomSource{}},
		{name: "zero window", client: client, scope: validScope, config: RedisUniformConfig{MinRequestsPerMinute: 2, MaxRequestsPerMinute: 56}, random: fixedIntegerRandomSource{}},
		{name: "partial-minute window", client: client, scope: validScope, config: RedisUniformConfig{MinRequestsPerMinute: 2, MaxRequestsPerMinute: 56, Window: 90 * time.Second}, random: fixedIntegerRandomSource{}},
		{name: "nil random", client: client, scope: validScope, config: validConfig},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := newRedisUniform(test.client, test.scope, test.config, test.random); err == nil {
				t.Fatal("newRedisUniform() error = nil")
			}
		})
	}
}

func newTestRedisUniform(
	t *testing.T,
	client redis.UniversalClient,
	scope RedisUniformScope,
	config RedisUniformConfig,
	random integerRandomSource,
) *RedisUniform {
	t.Helper()
	schedule, err := newRedisUniform(client, scope, config, random)
	if err != nil {
		t.Fatalf("newRedisUniform() error = %v", err)
	}
	return schedule
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

type fixedIntegerRandomSource struct {
	value int
}

func (s fixedIntegerRandomSource) IntN(n int) int {
	if s.value < 0 || s.value >= n {
		panic("test random value is outside IntN range")
	}
	return s.value
}
