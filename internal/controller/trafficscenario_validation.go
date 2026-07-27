package controller

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
)

const (
	maxRunnerCPU              = "1"
	maxRunnerMemory           = "512Mi"
	maxRunnerEphemeralStorage = "1Gi"
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
	if err := validateRunnerResources(scenario); err != nil {
		return err
	}
	if err := scenarioRunnerConfig(scenario).Validate(); err != nil {
		return fmt.Errorf("spec: %w", err)
	}
	return nil
}

func validateRunnerResources(scenario *trafficv1alpha1.TrafficScenario) error {
	if scenario.Spec.Runner == nil || scenario.Spec.Runner.Resources == nil {
		return nil
	}

	overrides := scenario.Spec.Runner.Resources
	if err := validateRunnerResourceList("requests", overrides.Requests); err != nil {
		return err
	}
	if err := validateRunnerResourceList("limits", overrides.Limits); err != nil {
		return err
	}

	merged := runnerResources(scenario)
	for _, name := range []corev1.ResourceName{
		corev1.ResourceCPU,
		corev1.ResourceMemory,
		corev1.ResourceEphemeralStorage,
	} {
		request := merged.Requests[name]
		limit := merged.Limits[name]
		if request.Cmp(limit) > 0 {
			return fmt.Errorf(
				"spec.runner.resources.requests[%q] must not exceed limit (%s > %s)",
				name,
				request.String(),
				limit.String(),
			)
		}
	}
	return nil
}

func validateRunnerResourceList(field string, resources corev1.ResourceList) error {
	for name, quantity := range resources {
		if !isAllowedRunnerResource(name) {
			return fmt.Errorf(
				"spec.runner.resources.%s contains unsupported resource %q; use cpu, memory or ephemeral-storage",
				field,
				name,
			)
		}
		if quantity.Sign() < 0 {
			return fmt.Errorf(
				"spec.runner.resources.%s[%q] must not be negative",
				field,
				name,
			)
		}
		maximum := maxRunnerResource(name)
		if quantity.Cmp(maximum) > 0 {
			return fmt.Errorf(
				"spec.runner.resources.%s[%q] must not exceed %s",
				field,
				name,
				maximum.String(),
			)
		}
	}
	return nil
}

func maxRunnerResource(name corev1.ResourceName) resource.Quantity {
	switch name {
	case corev1.ResourceCPU:
		return resource.MustParse(maxRunnerCPU)
	case corev1.ResourceMemory:
		return resource.MustParse(maxRunnerMemory)
	case corev1.ResourceEphemeralStorage:
		return resource.MustParse(maxRunnerEphemeralStorage)
	default:
		panic(fmt.Sprintf("maximum requested for unsupported runner resource %q", name))
	}
}

func isAllowedRunnerResource(name corev1.ResourceName) bool {
	switch name {
	case corev1.ResourceCPU, corev1.ResourceMemory, corev1.ResourceEphemeralStorage:
		return true
	default:
		return false
	}
}
