package controller

import (
	"fmt"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
)

func validateScenario(scenario *trafficv1alpha1.TrafficScenario) error {
	if scenario.Spec.Storage != nil {
		switch scenario.Spec.Storage.Type {
		case "", trafficv1alpha1.StorageTypeMemory, trafficv1alpha1.StorageTypeRedis:
		default:
			return fmt.Errorf("spec.storage.type %q is unsupported; use memory or redis", scenario.Spec.Storage.Type)
		}
	}
	if scenario.Spec.Rate.Limiter != nil {
		switch scenario.Spec.Rate.Limiter.Type {
		case "", trafficv1alpha1.RateLimiterTypeLocal, trafficv1alpha1.RateLimiterTypeRedis:
		default:
			return fmt.Errorf(
				"spec.rate.limiter.type %q is unsupported; use local or redis",
				scenario.Spec.Rate.Limiter.Type,
			)
		}
	}
	if scenario.Spec.Rate.Profile != nil {
		switch scenario.Spec.Rate.Profile.Type {
		case "",
			trafficv1alpha1.RateProfileTypeFixed,
			trafficv1alpha1.RateProfileTypeUniform,
			trafficv1alpha1.RateProfileTypeBurst:
		default:
			return fmt.Errorf(
				"spec.rate.profile.type %q is unsupported; use fixed, uniform or burst",
				scenario.Spec.Rate.Profile.Type,
			)
		}
	}
	replicas := runnerReplicas(scenario)
	if replicas < 1 || replicas > 10 {
		return fmt.Errorf("spec.replicas must be between 1 and 10")
	}
	if replicas > 1 && rateLimiterBackend(scenario) != string(trafficv1alpha1.RateLimiterTypeRedis) {
		return fmt.Errorf("spec.replicas greater than 1 requires spec.rate.limiter.type redis")
	}
	if err := scenarioRunnerConfig(scenario).Validate(); err != nil {
		return fmt.Errorf("spec: %w", err)
	}
	return nil
}
