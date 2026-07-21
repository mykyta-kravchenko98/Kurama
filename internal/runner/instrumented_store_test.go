package runner

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInstrumentedStoreObservesPutResults(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("put failed")
	tests := []struct {
		name       string
		storeError error
		wantResult string
	}{
		{name: "success", wantResult: StoreResultSuccess},
		{name: "error", storeError: storeErr, wantResult: StoreResultError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			observer := &recordingStoreObserver{}
			underlying := &stubValueStore{putErr: test.storeError}
			store := newTestInstrumentedStore(t, underlying, observer)
			setInstrumentedStoreClock(store)

			err := store.Put(context.Background(), "hashes", "value")
			if !errors.Is(err, test.storeError) {
				t.Fatalf("Put() error = %v, want %v", err, test.storeError)
			}
			assertStoreObservation(t, observer.single(t), StoreOperationObservation{
				Backend: "redis", Store: "hashes", Operation: StoreOperationPut,
				Result: test.wantResult, Duration: 5 * time.Millisecond,
			})
		})
	}
}

func TestInstrumentedStoreObservesRandomResults(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("random failed")
	tests := []struct {
		name       string
		value      string
		ok         bool
		storeError error
		wantResult string
	}{
		{name: "success", value: "captured", ok: true, wantResult: StoreResultSuccess},
		{name: "empty", wantResult: StoreResultEmpty},
		{name: "error", storeError: storeErr, wantResult: StoreResultError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			observer := &recordingStoreObserver{}
			underlying := &stubValueStore{randomValue: test.value, randomOK: test.ok, randomErr: test.storeError}
			store := newTestInstrumentedStore(t, underlying, observer)
			setInstrumentedStoreClock(store)

			value, ok, err := store.Random(context.Background(), "hashes")
			if value != test.value || ok != test.ok || !errors.Is(err, test.storeError) {
				t.Fatalf("Random() = (%q, %t, %v), want (%q, %t, %v)", value, ok, err, test.value, test.ok, test.storeError)
			}
			assertStoreObservation(t, observer.single(t), StoreOperationObservation{
				Backend: "redis", Store: "hashes", Operation: StoreOperationRandom,
				Result: test.wantResult, Duration: 5 * time.Millisecond,
			})
		})
	}
}

func TestNewInstrumentedStoreValidatesDependencies(t *testing.T) {
	t.Parallel()

	store := &stubValueStore{}
	observer := &recordingStoreObserver{}
	tests := []struct {
		name     string
		store    ValueStore
		backend  string
		observer StoreOperationObserver
	}{
		{name: "nil store", backend: "redis", observer: observer},
		{name: "empty backend", store: store, observer: observer},
		{name: "nil observer", store: store, backend: "redis"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewInstrumentedStore(test.store, test.backend, test.observer); err == nil {
				t.Fatal("NewInstrumentedStore() error = nil")
			}
		})
	}
}

type stubValueStore struct {
	putErr      error
	randomValue string
	randomOK    bool
	randomErr   error
}

func (s *stubValueStore) Put(context.Context, string, string) error {
	return s.putErr
}

func (s *stubValueStore) Random(context.Context, string) (string, bool, error) {
	return s.randomValue, s.randomOK, s.randomErr
}

type recordingStoreObserver struct {
	observations []StoreOperationObservation
}

func (r *recordingStoreObserver) ObserveStoreOperation(_ context.Context, observation StoreOperationObservation) {
	r.observations = append(r.observations, observation)
}

func (r *recordingStoreObserver) single(t *testing.T) StoreOperationObservation {
	t.Helper()
	if len(r.observations) != 1 {
		t.Fatalf("observations = %#v, want exactly one", r.observations)
	}
	return r.observations[0]
}

func newTestInstrumentedStore(t *testing.T, store ValueStore, observer StoreOperationObserver) *InstrumentedStore {
	t.Helper()
	instrumented, err := NewInstrumentedStore(store, "redis", observer)
	if err != nil {
		t.Fatalf("NewInstrumentedStore() error = %v", err)
	}
	return instrumented
}

func setInstrumentedStoreClock(store *InstrumentedStore) {
	times := []time.Time{time.Unix(0, 0), time.Unix(0, int64(5*time.Millisecond))}
	store.now = func() time.Time {
		current := times[0]
		times = times[1:]
		return current
	}
}

func assertStoreObservation(t *testing.T, got, want StoreOperationObservation) {
	t.Helper()
	if got != want {
		t.Fatalf("observation = %#v, want %#v", got, want)
	}
}
