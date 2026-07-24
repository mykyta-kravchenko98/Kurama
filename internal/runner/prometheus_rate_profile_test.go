package runner

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRegisterPrometheusRateProfileMetricsExportsEffectiveBurstConfig(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewPedanticRegistry()
	config := &RateProfileConfig{
		Type:         "burst",
		MinBurstSize: 10,
		MaxBurstSize: 25,
	}
	if err := RegisterPrometheusRateProfileMetrics(registry, config); err != nil {
		t.Fatalf("RegisterPrometheusRateProfileMetrics() error = %v", err)
	}

	expected := `
# HELP kurama_rate_profile_burst_size Effective configured burst size bounds used by the Kurama runner.
# TYPE kurama_rate_profile_burst_size gauge
kurama_rate_profile_burst_size{bound="max"} 25
kurama_rate_profile_burst_size{bound="min"} 10
# HELP kurama_rate_profile_delay_divisor Effective delay divisor used between requests within a Kurama burst.
# TYPE kurama_rate_profile_delay_divisor gauge
kurama_rate_profile_delay_divisor 10
`
	if err := testutil.GatherAndCompare(
		registry,
		strings.NewReader(expected),
		"kurama_rate_profile_burst_size",
		"kurama_rate_profile_delay_divisor",
	); err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
}

func TestRegisterPrometheusRateProfileMetricsUsesConfiguredDelayDivisor(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	config := &RateProfileConfig{
		Type:         "burst",
		MinBurstSize: 2,
		MaxBurstSize: 5,
		DelayDivisor: 4,
	}
	if err := RegisterPrometheusRateProfileMetrics(registry, config); err != nil {
		t.Fatalf("RegisterPrometheusRateProfileMetrics() error = %v", err)
	}

	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for _, family := range metricFamilies {
		if family.GetName() == "kurama_rate_profile_delay_divisor" {
			if got := family.GetMetric()[0].GetGauge().GetValue(); got != 4 {
				t.Fatalf("delay divisor metric = %v, want 4", got)
			}
			return
		}
	}
	t.Fatal("delay divisor metric was not registered")
}

func TestRegisterPrometheusRateProfileMetricsSkipsNonBurstProfiles(t *testing.T) {
	t.Parallel()

	for _, config := range []*RateProfileConfig{nil, {}, {Type: "fixed"}, {Type: "uniform"}} {
		registry := prometheus.NewRegistry()
		if err := RegisterPrometheusRateProfileMetrics(registry, config); err != nil {
			t.Fatalf("RegisterPrometheusRateProfileMetrics() error = %v", err)
		}
		metricFamilies, err := registry.Gather()
		if err != nil {
			t.Fatalf("Gather() error = %v", err)
		}
		if len(metricFamilies) != 0 {
			t.Fatalf("registered %d metric families for config %#v", len(metricFamilies), config)
		}
	}
}

func TestRegisterPrometheusRateProfileMetricsValidatesRegistererAndDuplicates(t *testing.T) {
	t.Parallel()

	config := &RateProfileConfig{Type: "burst", MinBurstSize: 2, MaxBurstSize: 5}
	if err := RegisterPrometheusRateProfileMetrics(nil, config); err == nil {
		t.Fatal("RegisterPrometheusRateProfileMetrics(nil) error = nil")
	}

	registry := prometheus.NewRegistry()
	if err := RegisterPrometheusRateProfileMetrics(registry, config); err != nil {
		t.Fatalf("first RegisterPrometheusRateProfileMetrics() error = %v", err)
	}
	if err := RegisterPrometheusRateProfileMetrics(registry, config); err == nil {
		t.Fatal("duplicate RegisterPrometheusRateProfileMetrics() error = nil")
	}
}
