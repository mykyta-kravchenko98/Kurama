package controller

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

// RedisCleanupObserver records finalizer cleanups that were abandoned after
// their grace period so TrafficScenario deletion could continue.
type RedisCleanupObserver interface {
	ObserveAbandoned()
}

type prometheusRedisCleanupObserver struct {
	abandoned prometheus.Counter
}

// NewPrometheusRedisCleanupObserver registers controller cleanup metrics.
func NewPrometheusRedisCleanupObserver(
	registerer prometheus.Registerer,
) (RedisCleanupObserver, error) {
	if registerer == nil {
		return nil, fmt.Errorf("prometheus registerer must not be nil")
	}
	abandoned := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "kurama_redis_cleanup_abandoned_total",
		Help: "Number of TrafficScenario Redis cleanups abandoned after the finalizer grace period.",
	})
	if err := registerer.Register(abandoned); err != nil {
		return nil, fmt.Errorf("register Redis cleanup metrics: %w", err)
	}
	return &prometheusRedisCleanupObserver{abandoned: abandoned}, nil
}

func (o *prometheusRedisCleanupObserver) ObserveAbandoned() {
	o.abandoned.Inc()
}
