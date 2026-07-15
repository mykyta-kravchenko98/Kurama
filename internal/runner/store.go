package runner

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
)

var (
	ErrStoreNotFound   = errors.New("store not found")
	ErrEmptyStoreValue = errors.New("store value must not be empty")
)

// ValueStore is the executor-facing boundary for scenario state. Context and
// errors are part of the contract so a future remote implementation, such as
// Redis, can report failures and honour cancellation without changing callers.
type ValueStore interface {
	Random(ctx context.Context, store string) (value string, ok bool, err error)
	Put(ctx context.Context, store, value string) error
}

// MemoryStore keeps independent, bounded value pools for one runner process.
// Once a pool reaches capacity, Put replaces its oldest entry in O(1) time.
type MemoryStore struct {
	stores map[string]*memoryPool
}

type memoryPool struct {
	mu     sync.RWMutex
	values []string
	next   int
	limit  int
}

func NewMemoryStore(configs []StoreConfig) (*MemoryStore, error) {
	store := &MemoryStore{stores: make(map[string]*memoryPool, len(configs))}
	for i, config := range configs {
		if err := validateName(config.Name); err != nil {
			return nil, fmt.Errorf("stores[%d].name: %w", i, err)
		}
		if config.Capacity < 1 || config.Capacity > MaxStoreCapacity {
			return nil, fmt.Errorf("stores[%d].capacity must be between 1 and %d", i, MaxStoreCapacity)
		}
		if _, exists := store.stores[config.Name]; exists {
			return nil, fmt.Errorf("stores[%d].name %q is duplicated", i, config.Name)
		}
		store.stores[config.Name] = &memoryPool{
			limit: config.Capacity,
		}
	}
	return store, nil
}

func (s *MemoryStore) Put(ctx context.Context, store, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if value == "" {
		return ErrEmptyStoreValue
	}

	pool, exists := s.stores[store]
	if !exists {
		return fmt.Errorf("%w: %q", ErrStoreNotFound, store)
	}
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if len(pool.values) < pool.limit {
		pool.values = append(pool.values, value)
		return nil
	}
	pool.values[pool.next] = value
	pool.next = (pool.next + 1) % pool.limit
	return nil
}

func (s *MemoryStore) Random(ctx context.Context, store string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}

	pool, exists := s.stores[store]
	if !exists {
		return "", false, fmt.Errorf("%w: %q", ErrStoreNotFound, store)
	}
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	if len(pool.values) == 0 {
		return "", false, nil
	}
	return pool.values[rand.IntN(len(pool.values))], true, nil
}
