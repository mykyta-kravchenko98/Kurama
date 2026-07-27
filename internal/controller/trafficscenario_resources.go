package controller

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
)

const (
	componentLabel                = "app.kubernetes.io/component"
	scenarioLabel                 = "traffic.kurama.dev/scenario"
	scenarioConfigKey             = "scenario.json"
	configHashAnnotation          = "traffic.kurama.dev/config-hash"
	runnerRevisionHistoryLimit    = int32(5)
	runnerProgressDeadlineSeconds = int32(120)
)

func desiredConfigMap(
	scenario *trafficv1alpha1.TrafficScenario,
	name, config string,
) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   scenario.Namespace,
			Name:        name,
			Labels:      labels(scenario),
			Annotations: scenarioAnnotations(scenario),
		},
		Data: map[string]string{scenarioConfigKey: config},
	}
}

func desiredDeployment(
	scenario *trafficv1alpha1.TrafficScenario,
	name, image, imagePullSecret, redisAddress, config string,
) *appsv1.Deployment {
	labels := labels(scenario)
	podAnnotations := map[string]string{
		configHashAnnotation:   configHash(config),
		"prometheus.io/scrape": "true",
		"prometheus.io/port":   fmt.Sprintf("%d", runner.MetricsPort),
		"prometheus.io/path":   runner.MetricsPath,
	}
	for key, value := range scenarioAnnotations(scenario) {
		podAnnotations[key] = value
	}
	podSpec := corev1.PodSpec{
		AutomountServiceAccountToken: ptr.To(false),
		EnableServiceLinks:           ptr.To(false),
		SecurityContext:              runnerPodSecurityContext(),
		Containers: []corev1.Container{{
			Name:            "runner",
			Image:           image,
			Command:         []string{"/app/runner"},
			Env:             runnerEnvironment(scenario, redisAddress),
			Resources:       runnerResources(scenario),
			SecurityContext: runnerContainerSecurityContext(),
			VolumeMounts:    []corev1.VolumeMount{{Name: "scenario", MountPath: "/etc/kurama", ReadOnly: true}},
			Ports: []corev1.ContainerPort{{
				Name:          runner.MetricsPortName,
				ContainerPort: runner.MetricsPort,
				Protocol:      corev1.ProtocolTCP,
			}},
			StartupProbe:   runnerHTTPProbe(runner.HealthPath, 2, 30),
			LivenessProbe:  runnerHTTPProbe(runner.HealthPath, 10, 3),
			ReadinessProbe: runnerHTTPProbe(runner.ReadinessPath, 5, 3),
		}},
		Volumes: []corev1.Volume{{
			Name: "scenario",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: name},
				},
			},
		}},
	}
	if imagePullSecret != "" {
		podSpec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: imagePullSecret}}
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   scenario.Namespace,
			Name:        name,
			Labels:      labels,
			Annotations: scenarioAnnotations(scenario),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas:                ptr.To(runnerReplicas(scenario)),
			RevisionHistoryLimit:    ptr.To(runnerRevisionHistoryLimit),
			ProgressDeadlineSeconds: ptr.To(runnerProgressDeadlineSeconds),
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: ptr.To(intstr.FromInt32(0)),
					MaxSurge:       ptr.To(intstr.FromInt32(1)),
				},
			},
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: podAnnotations,
				},
				Spec: podSpec,
			},
		},
	}
}

func runnerHTTPProbe(path string, periodSeconds, failureThreshold int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   path,
				Port:   intstr.FromString(runner.MetricsPortName),
				Scheme: corev1.URISchemeHTTP,
			},
		},
		TimeoutSeconds:   1,
		PeriodSeconds:    periodSeconds,
		SuccessThreshold: 1,
		FailureThreshold: failureThreshold,
	}
}

func runnerEnvironment(scenario *trafficv1alpha1.TrafficScenario, redisAddress string) []corev1.EnvVar {
	backend := storageBackend(scenario)
	environment := []corev1.EnvVar{{Name: runner.StoreBackendEnv, Value: backend}}
	if !requiresRedis(scenario) {
		return environment
	}
	return append(environment,
		corev1.EnvVar{Name: runner.RedisAddressEnv, Value: redisAddress},
		corev1.EnvVar{
			Name: runner.NamespaceEnv,
			ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{
				APIVersion: "v1",
				FieldPath:  "metadata.namespace",
			}},
		},
		corev1.EnvVar{Name: runner.ScenarioEnv, Value: scenario.Name},
		corev1.EnvVar{Name: runner.ScenarioUIDEnv, Value: string(scenario.UID)},
	)
}
