package controller

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
)

const (
	defaultRunnerCPURequest              = "25m"
	defaultRunnerMemoryRequest           = "32Mi"
	defaultRunnerEphemeralStorageRequest = "16Mi"
	defaultRunnerCPULimit                = "500m"
	defaultRunnerMemoryLimit             = "128Mi"
	defaultRunnerEphemeralStorageLimit   = "64Mi"
)

func runnerPodSecurityContext() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		RunAsNonRoot: ptr.To(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

func runnerContainerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		Privileged:               ptr.To(false),
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

func runnerResources(scenario *trafficv1alpha1.TrafficScenario) corev1.ResourceRequirements {
	resources := defaultRunnerResources()
	if scenario.Spec.Runner == nil || scenario.Spec.Runner.Resources == nil {
		return resources
	}

	configured := scenario.Spec.Runner.Resources.DeepCopy()
	if configured.Requests == nil {
		configured.Requests = corev1.ResourceList{}
	}
	if configured.Limits == nil {
		configured.Limits = corev1.ResourceList{}
	}
	mergeMissingResources(configured.Requests, resources.Requests)
	mergeMissingResources(configured.Limits, resources.Limits)
	return corev1.ResourceRequirements{
		Requests: configured.Requests,
		Limits:   configured.Limits,
	}
}

func defaultRunnerResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:              resource.MustParse(defaultRunnerCPURequest),
			corev1.ResourceMemory:           resource.MustParse(defaultRunnerMemoryRequest),
			corev1.ResourceEphemeralStorage: resource.MustParse(defaultRunnerEphemeralStorageRequest),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:              resource.MustParse(defaultRunnerCPULimit),
			corev1.ResourceMemory:           resource.MustParse(defaultRunnerMemoryLimit),
			corev1.ResourceEphemeralStorage: resource.MustParse(defaultRunnerEphemeralStorageLimit),
		},
	}
}

func mergeMissingResources(target, defaults corev1.ResourceList) {
	for name, quantity := range defaults {
		if _, exists := target[name]; exists {
			continue
		}
		target[name] = quantity.DeepCopy()
	}
}
