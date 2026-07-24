package runner

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner/profile"
)

// RegisterPrometheusRateProfileMetrics exports the effective burst profile
// configuration used by this runner. Non-burst profiles do not expose these
// metrics because zero-valued burst bounds would be misleading.
func RegisterPrometheusRateProfileMetrics(
	registerer prometheus.Registerer,
	config *RateProfileConfig,
) error {
	if registerer == nil {
		return fmt.Errorf("prometheus registerer must not be nil")
	}
	if config == nil || config.Type != profile.TypeBurst {
		return nil
	}

	burstSize := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "kurama",
		Subsystem: "rate_profile",
		Name:      "burst_size",
		Help:      "Effective configured burst size bounds used by the Kurama runner.",
	}, []string{"bound"})
	delayDivisor := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "kurama",
		Subsystem: "rate_profile",
		Name:      "delay_divisor",
		Help:      "Effective delay divisor used between requests within a Kurama burst.",
	})

	burstSize.WithLabelValues("min").Set(float64(config.MinBurstSize))
	burstSize.WithLabelValues("max").Set(float64(config.MaxBurstSize))
	effectiveDelayDivisor := config.DelayDivisor
	if effectiveDelayDivisor == 0 {
		effectiveDelayDivisor = profile.DefaultBurstDelayDivisor
	}
	delayDivisor.Set(float64(effectiveDelayDivisor))

	if err := registerer.Register(burstSize); err != nil {
		return fmt.Errorf("register rate profile burst size metric: %w", err)
	}
	if err := registerer.Register(delayDivisor); err != nil {
		registerer.Unregister(burstSize)
		return fmt.Errorf("register rate profile delay divisor metric: %w", err)
	}
	return nil
}
