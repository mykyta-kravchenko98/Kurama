package runner

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
)

func TestMemoryStorePutAndRandom(t *testing.T) {
	t.Parallel()
	store := newTestMemoryStore(t, StoreConfig{Name: "hashes", Capacity: 2})
	ctx := context.Background()
	for _, value := range []string{"first", "second"} {
		if err := store.Put(ctx, "hashes", value); err != nil {
			t.Fatalf("Put(%q) error: %v", value, err)
		}
	}

	value, ok, err := store.Random(ctx, "hashes")
	if err != nil || !ok || (value != "first" && value != "second") {
		t.Fatalf("Random() = %q, %v, %v", value, ok, err)
	}
}

func TestMemoryStoreEvictsOldestValueAtCapacity(t *testing.T) {
	t.Parallel()
	store := newTestMemoryStore(t, StoreConfig{Name: "hashes", Capacity: 2})
	ctx := context.Background()
	for _, value := range []string{"oldest", "second", "newest"} {
		if err := store.Put(ctx, "hashes", value); err != nil {
			t.Fatalf("Put(%q) error: %v", value, err)
		}
	}

	pool := store.stores["hashes"]
	pool.mu.RLock()
	values := slices.Clone(pool.values)
	pool.mu.RUnlock()
	if !slices.Equal(values, []string{"newest", "second"}) {
		t.Fatalf("stored values = %v; oldest value was not evicted", values)
	}
}

func TestMemoryStoreKeepsNamedPoolsIndependent(t *testing.T) {
	t.Parallel()
	store := newTestMemoryStore(t,
		StoreConfig{Name: "hashes", Capacity: 1},
		StoreConfig{Name: "tokens", Capacity: 1},
	)
	ctx := context.Background()
	if err := store.Put(ctx, "hashes", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "tokens", "token"); err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]string{"hashes": "hash", "tokens": "token"} {
		got, ok, err := store.Random(ctx, name)
		if err != nil || !ok || got != want {
			t.Errorf("Random(%q) = %q, %v, %v; want %q", name, got, ok, err, want)
		}
	}
}

func TestMemoryStoreReportsEmptyAndUnknownPools(t *testing.T) {
	t.Parallel()
	store := newTestMemoryStore(t, StoreConfig{Name: "hashes", Capacity: 1})
	value, ok, err := store.Random(context.Background(), "hashes")
	if err != nil || ok || value != "" {
		t.Fatalf("empty Random() = %q, %v, %v", value, ok, err)
	}
	_, _, err = store.Random(context.Background(), "missing")
	if !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("unknown Random() error = %v", err)
	}
	if err := store.Put(context.Background(), "missing", "value"); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("unknown Put() error = %v", err)
	}
}

func TestMemoryStoreRejectsEmptyValueAndCancelledContext(t *testing.T) {
	t.Parallel()
	store := newTestMemoryStore(t, StoreConfig{Name: "hashes", Capacity: 1})
	if err := store.Put(context.Background(), "hashes", ""); !errors.Is(err, ErrEmptyStoreValue) {
		t.Fatalf("empty Put() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Put(ctx, "hashes", "value"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Put() error = %v", err)
	}
	if _, _, err := store.Random(ctx, "hashes"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Random() error = %v", err)
	}
}

func TestNewMemoryStoreValidatesConfiguration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		configs []StoreConfig
	}{
		{name: "invalid name", configs: []StoreConfig{{Name: "Invalid", Capacity: 1}}},
		{name: "invalid capacity", configs: []StoreConfig{{Name: "hashes", Capacity: 0}}},
		{name: "duplicate", configs: []StoreConfig{{Name: "hashes", Capacity: 1}, {Name: "hashes", Capacity: 2}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewMemoryStore(test.configs); err == nil {
				t.Fatal("NewMemoryStore() error = nil")
			}
		})
	}
}

func TestMemoryStoreSupportsConcurrentAccess(t *testing.T) {
	t.Parallel()
	store := newTestMemoryStore(t, StoreConfig{Name: "hashes", Capacity: 64})
	ctx := context.Background()
	var workers sync.WaitGroup
	for worker := range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for sequence := range 100 {
				if err := store.Put(ctx, "hashes", fmt.Sprintf("%d-%d", worker, sequence)); err != nil {
					t.Errorf("Put() error: %v", err)
					return
				}
				if _, _, err := store.Random(ctx, "hashes"); err != nil {
					t.Errorf("Random() error: %v", err)
					return
				}
			}
		}()
	}
	workers.Wait()
}

func newTestMemoryStore(t *testing.T, configs ...StoreConfig) *MemoryStore {
	t.Helper()
	store, err := NewMemoryStore(configs)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
