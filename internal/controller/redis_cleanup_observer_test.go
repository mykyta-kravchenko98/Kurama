package controller

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPrometheusRedisCleanupObserverRecordsAbandonedCleanup(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewPedanticRegistry()
	observer, err := NewPrometheusRedisCleanupObserver(registry)
	if err != nil {
		t.Fatalf("NewPrometheusRedisCleanupObserver() error = %v", err)
	}
	observer.ObserveAbandoned()

	const want = `
# HELP kurama_redis_cleanup_abandoned_total Number of TrafficScenario Redis cleanups abandoned after the finalizer grace period.
# TYPE kurama_redis_cleanup_abandoned_total counter
kurama_redis_cleanup_abandoned_total 1
`
	if err := testutil.GatherAndCompare(
		registry,
		strings.NewReader(want),
		"kurama_redis_cleanup_abandoned_total",
	); err != nil {
		t.Fatalf("cleanup metrics mismatch: %v", err)
	}
}

func TestPrometheusRedisCleanupObserverValidatesRegistererAndDuplicates(t *testing.T) {
	t.Parallel()

	if _, err := NewPrometheusRedisCleanupObserver(nil); err == nil {
		t.Fatal("nil registerer error = nil")
	}
	registry := prometheus.NewRegistry()
	if _, err := NewPrometheusRedisCleanupObserver(registry); err != nil {
		t.Fatalf("first registration error = %v", err)
	}
	if _, err := NewPrometheusRedisCleanupObserver(registry); err == nil {
		t.Fatal("duplicate registration error = nil")
	}
}
