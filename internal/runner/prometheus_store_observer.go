package runner

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

type PrometheusStoreObserver struct {
	operations *prometheus.CounterVec
	duration   *prometheus.HistogramVec
}

var _ StoreOperationObserver = (*PrometheusStoreObserver)(nil)

func NewPrometheusStoreObserver(registerer prometheus.Registerer) (*PrometheusStoreObserver, error) {
	if registerer == nil {
		return nil, fmt.Errorf("prometheus registerer must not be nil")
	}

	operations := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kurama",
		Subsystem: "store",
		Name:      "operations_total",
		Help:      "Total number of Kurama value store operations.",
	}, []string{"backend", "store", "operation", "result"})
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "kurama",
		Subsystem: "store",
		Name:      "operation_duration_seconds",
		Help:      "Duration of Kurama value store operations in seconds.",
		Buckets:   prometheus.ExponentialBuckets(0.0001, 2, 15),
	}, []string{"backend", "store", "operation", "result"})

	if err := registerer.Register(operations); err != nil {
		return nil, fmt.Errorf("register store operations metric: %w", err)
	}
	if err := registerer.Register(duration); err != nil {
		registerer.Unregister(operations)
		return nil, fmt.Errorf("register store operation duration metric: %w", err)
	}
	return &PrometheusStoreObserver{operations: operations, duration: duration}, nil
}

func (o *PrometheusStoreObserver) ObserveStoreOperation(_ context.Context, observation StoreOperationObservation) {
	labels := []string{observation.Backend, observation.Store, observation.Operation, observation.Result}
	o.operations.WithLabelValues(labels...).Inc()
	o.duration.WithLabelValues(labels...).Observe(observation.Duration.Seconds())
}
