package runner

import (
	"context"
	"fmt"
	"time"
)

const (
	StoreOperationPut    = "put"
	StoreOperationRandom = "random"

	StoreResultSuccess = "success"
	StoreResultEmpty   = "empty"
	StoreResultError   = "error"
)

// StoreOperationObservation is a bounded-cardinality description of one
// ValueStore call. Captured values are deliberately excluded from metrics.
type StoreOperationObservation struct {
	Backend   string
	Store     string
	Operation string
	Result    string
	Duration  time.Duration
}

// StoreOperationObserver receives ValueStore measurements without being able
// to fail the workload. The context can later carry trace exemplars into a
// concrete metrics implementation.
type StoreOperationObserver interface {
	ObserveStoreOperation(ctx context.Context, observation StoreOperationObservation)
}

// InstrumentedStore decorates any ValueStore while preserving its return
// values and errors exactly.
type InstrumentedStore struct {
	store    ValueStore
	backend  string
	observer StoreOperationObserver
	now      func() time.Time
}

var _ ValueStore = (*InstrumentedStore)(nil)

func NewInstrumentedStore(store ValueStore, backend string, observer StoreOperationObserver) (*InstrumentedStore, error) {
	if store == nil {
		return nil, fmt.Errorf("value store must not be nil")
	}
	if backend == "" {
		return nil, fmt.Errorf("store backend must not be empty")
	}
	if observer == nil {
		return nil, fmt.Errorf("store operation observer must not be nil")
	}
	return &InstrumentedStore{
		store:    store,
		backend:  backend,
		observer: observer,
		now:      time.Now,
	}, nil
}

func (s *InstrumentedStore) Put(ctx context.Context, store, value string) error {
	started := s.now()
	err := s.store.Put(ctx, store, value)
	result := StoreResultSuccess
	if err != nil {
		result = StoreResultError
	}
	s.observe(ctx, store, StoreOperationPut, result, started)
	return err
}

func (s *InstrumentedStore) Random(ctx context.Context, store string) (string, bool, error) {
	started := s.now()
	value, ok, err := s.store.Random(ctx, store)
	result := StoreResultSuccess
	if err != nil {
		result = StoreResultError
	} else if !ok {
		result = StoreResultEmpty
	}
	s.observe(ctx, store, StoreOperationRandom, result, started)
	return value, ok, err
}

func (s *InstrumentedStore) observe(ctx context.Context, store, operation, result string, started time.Time) {
	s.observer.ObserveStoreOperation(ctx, StoreOperationObservation{
		Backend:   s.backend,
		Store:     store,
		Operation: operation,
		Result:    result,
		Duration:  s.now().Sub(started),
	})
}
