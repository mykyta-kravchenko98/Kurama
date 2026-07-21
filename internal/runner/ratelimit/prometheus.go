package ratelimit

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

type PrometheusObserver struct {
	acquisitions *prometheus.CounterVec
	duration     *prometheus.HistogramVec
}

var _ Observer = (*PrometheusObserver)(nil)

func NewPrometheusObserver(registerer prometheus.Registerer) (*PrometheusObserver, error) {
	if registerer == nil {
		return nil, fmt.Errorf("prometheus registerer must not be nil")
	}

	acquisitions := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kurama",
		Subsystem: "rate_limiter",
		Name:      "acquisitions_total",
		Help:      "Total number of Kurama rate limiter acquisition attempts.",
	}, []string{"backend", "result"})
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "kurama",
		Subsystem: "rate_limiter",
		Name:      "acquisition_duration_seconds",
		Help:      "Duration of Kurama rate limiter acquisitions in seconds.",
		Buckets:   prometheus.ExponentialBuckets(0.0001, 2, 15),
	}, []string{"backend", "result"})

	if err := registerer.Register(acquisitions); err != nil {
		return nil, fmt.Errorf("register rate limiter acquisitions metric: %w", err)
	}
	if err := registerer.Register(duration); err != nil {
		registerer.Unregister(acquisitions)
		return nil, fmt.Errorf("register rate limiter acquisition duration metric: %w", err)
	}
	return &PrometheusObserver{acquisitions: acquisitions, duration: duration}, nil
}

func (o *PrometheusObserver) ObserveRateLimit(_ context.Context, observation Observation) {
	labels := []string{observation.Backend, observation.Result}
	o.acquisitions.WithLabelValues(labels...).Inc()
	o.duration.WithLabelValues(labels...).Observe(observation.Duration.Seconds())
}
