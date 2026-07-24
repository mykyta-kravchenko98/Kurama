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
	configHashAnnotation          = "traffic.kurama.dev/config-hash"
	runnerRevisionHistoryLimit    = int32(5)
	runnerProgressDeadlineSeconds = int32(120)
)

func desiredConfigMap(scenario *trafficv1alpha1.TrafficScenario, name string) *corev1.ConfigMap {
	config := scenarioConfigJSON(scenario)
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: scenario.Namespace, Name: name, Labels: labels(scenario)},
		Data:       map[string]string{"scenario.json": config},
	}
}

func desiredDeployment(
	scenario *trafficv1alpha1.TrafficScenario,
	name, image, imagePullSecret, redisAddress string,
) *appsv1.Deployment {
	labels := labels(scenario)
	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{{
			Name:         "runner",
			Image:        image,
			Command:      []string{"/app/runner"},
			Env:          runnerEnvironment(scenario, redisAddress),
			VolumeMounts: []corev1.VolumeMount{{Name: "scenario", MountPath: "/etc/kurama", ReadOnly: true}},
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
		ObjectMeta: metav1.ObjectMeta{Namespace: scenario.Namespace, Name: name, Labels: labels},
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
					Labels: labels,
					Annotations: map[string]string{
						configHashAnnotation:   configHash(scenarioConfigJSON(scenario)),
						"prometheus.io/scrape": "true",
						"prometheus.io/port":   fmt.Sprintf("%d", runner.MetricsPort),
						"prometheus.io/path":   runner.MetricsPath,
					},
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
				Path: path,
				Port: intstr.FromString(runner.MetricsPortName),
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
				FieldPath: "metadata.namespace",
			}},
		},
		corev1.EnvVar{Name: runner.ScenarioEnv, Value: scenario.Name},
	)
}

func labels(scenario *trafficv1alpha1.TrafficScenario) map[string]string {
	return map[string]string{componentLabel: "runner", scenarioLabel: scenario.Name}
}

func runnerName(scenarioName string) string {
	const suffix = "-runner"
	if len(scenarioName)+len(suffix) <= 63 {
		return scenarioName + suffix
	}
	return scenarioName[:63-len(suffix)] + suffix
}
