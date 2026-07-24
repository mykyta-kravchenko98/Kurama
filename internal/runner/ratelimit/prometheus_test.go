package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPrometheusObserverRecordsAcquisitionAndDuration(t *testing.T) {
	t.Parallel()
	registry := prometheus.NewPedanticRegistry()
	observer, err := NewPrometheusObserver(registry)
	if err != nil {
		t.Fatal(err)
	}
	observer.ObserveRateLimit(context.Background(), Observation{
		Backend:          "redis",
		Result:           ResultPartial,
		Duration:         3 * time.Millisecond,
		RequestedPermits: 5,
		GrantedPermits:   3,
	})

	if got := testutil.ToFloat64(observer.acquisitions.WithLabelValues("redis", ResultPartial)); got != 1 {
		t.Fatalf("acquisitions = %v; want 1", got)
	}
	if got := testutil.ToFloat64(observer.permitsRequested.WithLabelValues("redis")); got != 5 {
		t.Fatalf("requested permits = %v; want 5", got)
	}
	if got := testutil.ToFloat64(observer.permitsGranted.WithLabelValues("redis")); got != 3 {
		t.Fatalf("granted permits = %v; want 3", got)
	}
	if got := testutil.CollectAndCount(observer.duration, "kurama_rate_limiter_acquisition_duration_seconds"); got != 1 {
		t.Fatalf("duration metric count = %d; want 1", got)
	}
}

func TestNewPrometheusObserverValidatesAndRejectsDuplicateRegistration(t *testing.T) {
	t.Parallel()
	if _, err := NewPrometheusObserver(nil); err == nil {
		t.Fatal("NewPrometheusObserver(nil) error = nil")
	}
	registry := prometheus.NewRegistry()
	if _, err := NewPrometheusObserver(registry); err != nil {
		t.Fatalf("first NewPrometheusObserver() error = %v", err)
	}
	if _, err := NewPrometheusObserver(registry); err == nil {
		t.Fatal("duplicate NewPrometheusObserver() error = nil")
	}
}
