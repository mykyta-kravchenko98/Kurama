package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPrometheusStoreObserverRecordsOperationAndDuration(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewPedanticRegistry()
	observer, err := NewPrometheusStoreObserver(registry)
	if err != nil {
		t.Fatalf("NewPrometheusStoreObserver() error = %v", err)
	}
	observation := StoreOperationObservation{
		Backend: "redis", Store: "hashes", Operation: StoreOperationRandom,
		Result: StoreResultSuccess, Duration: 3 * time.Millisecond,
	}
	observer.ObserveStoreOperation(context.Background(), observation)

	expected := `
# HELP kurama_store_operation_duration_seconds Duration of Kurama value store operations in seconds.
# TYPE kurama_store_operation_duration_seconds histogram
kurama_store_operation_duration_seconds_bucket{backend="redis",operation="random",result="success",store="hashes",le="0.0001"} 0
kurama_store_operation_duration_seconds_bucket{backend="redis",operation="random",result="success",store="hashes",le="0.0002"} 0
kurama_store_operation_duration_seconds_bucket{backend="redis",operation="random",result="success",store="hashes",le="0.0004"} 0
kurama_store_operation_duration_seconds_bucket{backend="redis",operation="random",result="success",store="hashes",le="0.0008"} 0
kurama_store_operation_duration_seconds_bucket{backend="redis",operation="random",result="success",store="hashes",le="0.0016"} 0
kurama_store_operation_duration_seconds_bucket{backend="redis",operation="random",result="success",store="hashes",le="0.0032"} 1
kurama_store_operation_duration_seconds_bucket{backend="redis",operation="random",result="success",store="hashes",le="0.0064"} 1
kurama_store_operation_duration_seconds_bucket{backend="redis",operation="random",result="success",store="hashes",le="0.0128"} 1
kurama_store_operation_duration_seconds_bucket{backend="redis",operation="random",result="success",store="hashes",le="0.0256"} 1
kurama_store_operation_duration_seconds_bucket{backend="redis",operation="random",result="success",store="hashes",le="0.0512"} 1
kurama_store_operation_duration_seconds_bucket{backend="redis",operation="random",result="success",store="hashes",le="0.1024"} 1
kurama_store_operation_duration_seconds_bucket{backend="redis",operation="random",result="success",store="hashes",le="0.2048"} 1
kurama_store_operation_duration_seconds_bucket{backend="redis",operation="random",result="success",store="hashes",le="0.4096"} 1
kurama_store_operation_duration_seconds_bucket{backend="redis",operation="random",result="success",store="hashes",le="0.8192"} 1
kurama_store_operation_duration_seconds_bucket{backend="redis",operation="random",result="success",store="hashes",le="1.6384"} 1
kurama_store_operation_duration_seconds_bucket{backend="redis",operation="random",result="success",store="hashes",le="+Inf"} 1
kurama_store_operation_duration_seconds_sum{backend="redis",operation="random",result="success",store="hashes"} 0.003
kurama_store_operation_duration_seconds_count{backend="redis",operation="random",result="success",store="hashes"} 1
# HELP kurama_store_operations_total Total number of Kurama value store operations.
# TYPE kurama_store_operations_total counter
kurama_store_operations_total{backend="redis",operation="random",result="success",store="hashes"} 1
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected),
		"kurama_store_operations_total",
		"kurama_store_operation_duration_seconds",
	); err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
}

func TestNewPrometheusStoreObserverValidatesAndRejectsDuplicateRegistration(t *testing.T) {
	t.Parallel()

	if _, err := NewPrometheusStoreObserver(nil); err == nil {
		t.Fatal("NewPrometheusStoreObserver(nil) error = nil")
	}
	registry := prometheus.NewRegistry()
	if _, err := NewPrometheusStoreObserver(registry); err != nil {
		t.Fatalf("first NewPrometheusStoreObserver() error = %v", err)
	}
	if _, err := NewPrometheusStoreObserver(registry); err == nil {
		t.Fatal("duplicate NewPrometheusStoreObserver() error = nil")
	}
}
