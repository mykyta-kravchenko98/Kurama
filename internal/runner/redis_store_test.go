package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisStorePutRandomAndCapacity(t *testing.T) {
	t.Parallel()

	server, client := newTestRedis(t)
	store := newTestRedisStore(t, client, RedisStoreScope{Namespace: "shorturl", Scenario: "load"},
		StoreConfig{Name: "hashes", Capacity: 2},
	)

	ctx := context.Background()
	for _, value := range []string{"first", "second", "third"} {
		if err := store.Put(ctx, "hashes", value); err != nil {
			t.Fatalf("Put(%q) error = %v", value, err)
		}
	}

	values, err := server.List(store.key("hashes"))
	if err != nil {
		t.Fatalf("read Redis list: %v", err)
	}
	if len(values) != 2 || values[0] != "second" || values[1] != "third" {
		t.Fatalf("bounded Redis values = %v, want [second third]", values)
	}

	value, ok, err := store.Random(ctx, "hashes")
	if err != nil {
		t.Fatalf("Random() error = %v", err)
	}
	if !ok || (value != "second" && value != "third") {
		t.Fatalf("Random() = (%q, %t), want second or third", value, ok)
	}
}

func TestRedisStoreKeepsScopesAndNamedPoolsIndependent(t *testing.T) {
	t.Parallel()

	server, client := newTestRedis(t)
	configs := []StoreConfig{{Name: "hashes", Capacity: 2}, {Name: "tokens", Capacity: 2}}
	first := newTestRedisStore(t, client, RedisStoreScope{Namespace: "shorturl", Scenario: "first"}, configs...)
	second := newTestRedisStore(t, client, RedisStoreScope{Namespace: "shorturl", Scenario: "second"}, configs...)

	ctx := context.Background()
	if err := first.Put(ctx, "hashes", "first-hash"); err != nil {
		t.Fatalf("first hashes Put() error = %v", err)
	}
	if err := first.Put(ctx, "tokens", "first-token"); err != nil {
		t.Fatalf("first tokens Put() error = %v", err)
	}
	if err := second.Put(ctx, "hashes", "second-hash"); err != nil {
		t.Fatalf("second hashes Put() error = %v", err)
	}

	want := map[string]string{
		first.key("hashes"):  "first-hash",
		first.key("tokens"):  "first-token",
		second.key("hashes"): "second-hash",
	}
	for key, expected := range want {
		values, err := server.List(key)
		if err != nil {
			t.Fatalf("read Redis list %q: %v", key, err)
		}
		if len(values) != 1 || values[0] != expected {
			t.Errorf("Redis list %q = %v, want [%s]", key, values, expected)
		}
	}
}

func TestRedisStoreReportsEmptyUnknownAndInvalidValues(t *testing.T) {
	t.Parallel()

	_, client := newTestRedis(t)
	store := newTestRedisStore(t, client, RedisStoreScope{Namespace: "shorturl", Scenario: "load"},
		StoreConfig{Name: "hashes", Capacity: 1},
	)

	value, ok, err := store.Random(context.Background(), "hashes")
	if err != nil || ok || value != "" {
		t.Fatalf("empty Random() = (%q, %t, %v), want empty, false, nil", value, ok, err)
	}
	if _, _, err := store.Random(context.Background(), "missing"); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("unknown Random() error = %v, want ErrStoreNotFound", err)
	}
	if err := store.Put(context.Background(), "missing", "value"); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("unknown Put() error = %v, want ErrStoreNotFound", err)
	}
	if err := store.Put(context.Background(), "hashes", ""); !errors.Is(err, ErrEmptyStoreValue) {
		t.Fatalf("empty Put() error = %v, want ErrEmptyStoreValue", err)
	}
}

func TestRedisStoreHonoursCancellationAndReportsRedisErrors(t *testing.T) {
	t.Parallel()

	server, client := newTestRedis(t)
	store := newTestRedisStore(t, client, RedisStoreScope{Namespace: "shorturl", Scenario: "load"},
		StoreConfig{Name: "hashes", Capacity: 1},
	)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Put(cancelled, "hashes", "value"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Put() error = %v, want context.Canceled", err)
	}
	if _, _, err := store.Random(cancelled, "hashes"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Random() error = %v, want context.Canceled", err)
	}

	server.Close()
	if err := store.Put(context.Background(), "hashes", "value"); err == nil {
		t.Fatal("Put() after Redis shutdown error = nil")
	}
	if _, _, err := store.Random(context.Background(), "hashes"); err == nil {
		t.Fatal("Random() after Redis shutdown error = nil")
	}
}

func TestNewRedisStoreValidatesConfiguration(t *testing.T) {
	t.Parallel()

	_, client := newTestRedis(t)
	validScope := RedisStoreScope{Namespace: "shorturl", Scenario: "load"}
	tests := []struct {
		name    string
		client  redis.UniversalClient
		scope   RedisStoreScope
		configs []StoreConfig
	}{
		{name: "nil client", scope: validScope},
		{name: "empty namespace", client: client, scope: RedisStoreScope{Scenario: "load"}},
		{name: "empty scenario", client: client, scope: RedisStoreScope{Namespace: "shorturl"}},
		{name: "namespace colon", client: client, scope: RedisStoreScope{Namespace: "short:url", Scenario: "load"}},
		{name: "scenario colon", client: client, scope: RedisStoreScope{Namespace: "shorturl", Scenario: "lo:ad"}},
		{name: "invalid store name", client: client, scope: validScope, configs: []StoreConfig{{Name: "Hashes", Capacity: 1}}},
		{name: "invalid capacity", client: client, scope: validScope, configs: []StoreConfig{{Name: "hashes"}}},
		{name: "duplicate store", client: client, scope: validScope, configs: []StoreConfig{{Name: "hashes", Capacity: 1}, {Name: "hashes", Capacity: 1}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewRedisStore(test.client, test.scope, test.configs); err == nil {
				t.Fatal("NewRedisStore() error = nil")
			}
		})
	}
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

func newTestRedisStore(t *testing.T, client redis.UniversalClient, scope RedisStoreScope, configs ...StoreConfig) *RedisStore {
	t.Helper()
	store, err := NewRedisStore(client, scope, configs)
	if err != nil {
		t.Fatalf("NewRedisStore() error = %v", err)
	}
	return store
}
