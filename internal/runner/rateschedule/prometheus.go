package rateschedule

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

// PrometheusObserver exports rate schedule resolutions.
type PrometheusObserver struct {
	requestsPerMinute *prometheus.GaugeVec
	resolutions       *prometheus.CounterVec
	duration          *prometheus.HistogramVec
}

var _ Observer = (*PrometheusObserver)(nil)

// NewPrometheusObserver registers rate schedule metrics.
func NewPrometheusObserver(registerer prometheus.Registerer) (*PrometheusObserver, error) {
	if registerer == nil {
		return nil, fmt.Errorf("prometheus registerer must not be nil")
	}

	requestsPerMinute := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "kurama",
		Subsystem: "rate_schedule",
		Name:      "requests_per_minute",
		Help:      "Current requests-per-minute value selected by the Kurama rate schedule.",
	}, []string{"type"})
	resolutions := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kurama",
		Subsystem: "rate_schedule",
		Name:      "resolutions_total",
		Help:      "Total number of Kurama rate schedule resolution attempts.",
	}, []string{"type", "result"})
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "kurama",
		Subsystem: "rate_schedule",
		Name:      "resolution_duration_seconds",
		Help:      "Duration of Kurama rate schedule resolutions in seconds.",
		Buckets:   prometheus.ExponentialBuckets(0.0001, 2, 15),
	}, []string{"type", "result"})

	if err := registerer.Register(requestsPerMinute); err != nil {
		return nil, fmt.Errorf("register rate schedule requests-per-minute metric: %w", err)
	}
	if err := registerer.Register(resolutions); err != nil {
		registerer.Unregister(requestsPerMinute)
		return nil, fmt.Errorf("register rate schedule resolutions metric: %w", err)
	}
	if err := registerer.Register(duration); err != nil {
		registerer.Unregister(requestsPerMinute)
		registerer.Unregister(resolutions)
		return nil, fmt.Errorf("register rate schedule resolution duration metric: %w", err)
	}
	return &PrometheusObserver{
		requestsPerMinute: requestsPerMinute,
		resolutions:       resolutions,
		duration:          duration,
	}, nil
}

func (o *PrometheusObserver) ObserveRateSchedule(_ context.Context, observation Observation) {
	labels := []string{observation.Type, observation.Result}
	o.resolutions.WithLabelValues(labels...).Inc()
	o.duration.WithLabelValues(labels...).Observe(observation.Duration.Seconds())
	if observation.Result == ResultSuccess {
		o.requestsPerMinute.WithLabelValues(observation.Type).Set(float64(observation.RequestsPerMinute))
	}
}
