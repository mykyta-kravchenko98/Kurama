package rateschedule

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPrometheusObserverRecordsScheduleMetrics(t *testing.T) {
	t.Parallel()
	registry := prometheus.NewPedanticRegistry()
	observer, err := NewPrometheusObserver(registry)
	if err != nil {
		t.Fatal(err)
	}
	observer.ObserveRateSchedule(context.Background(), Observation{
		Type: "uniform", Result: ResultSuccess, RequestsPerMinute: 76, Duration: 3 * time.Millisecond,
	})

	if got := testutil.ToFloat64(observer.requestsPerMinute.WithLabelValues("uniform")); got != 76 {
		t.Fatalf("requests per minute = %v; want 76", got)
	}
	if got := testutil.ToFloat64(observer.resolutions.WithLabelValues("uniform", ResultSuccess)); got != 1 {
		t.Fatalf("resolutions = %v; want 1", got)
	}
	if got := testutil.CollectAndCount(observer.duration, "kurama_rate_schedule_resolution_duration_seconds"); got != 1 {
		t.Fatalf("duration metric count = %d; want 1", got)
	}
}

func TestPrometheusObserverKeepsLastRateAfterError(t *testing.T) {
	t.Parallel()
	observer, err := NewPrometheusObserver(prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	observer.ObserveRateSchedule(context.Background(), Observation{
		Type: "uniform", Result: ResultSuccess, RequestsPerMinute: 76,
	})
	observer.ObserveRateSchedule(context.Background(), Observation{
		Type: "uniform", Result: ResultError,
	})

	if got := testutil.ToFloat64(observer.requestsPerMinute.WithLabelValues("uniform")); got != 76 {
		t.Fatalf("requests per minute after error = %v; want 76", got)
	}
	if got := testutil.ToFloat64(observer.resolutions.WithLabelValues("uniform", ResultError)); got != 1 {
		t.Fatalf("error resolutions = %v; want 1", got)
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
