// Package controller reconciles TrafficScenario resources into runner Pods.
package controller

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
)

const (
	componentLabel       = "app.kubernetes.io/component"
	scenarioLabel        = "traffic.kurama.dev/scenario"
	configHashAnnotation = "traffic.kurama.dev/config-hash"
)

// TrafficScenarioReconciler turns every active scenario into exactly one
// runner Deployment and its ConfigMap. RunnerImage is intentionally manager
// configuration rather than CRD data: image provenance remains GitOps-owned.
type TrafficScenarioReconciler struct {
	client.Client
	Scheme                *runtime.Scheme
	RunnerImage           string
	RunnerImagePullSecret string
	RedisAddress          string
}

// +kubebuilder:rbac:groups=traffic.kurama.dev,resources=trafficscenarios,verbs=get;list;watch
// +kubebuilder:rbac:groups=traffic.kurama.dev,resources=trafficscenarios/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

func (r *TrafficScenarioReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var scenario trafficv1alpha1.TrafficScenario
	if err := r.Get(ctx, req.NamespacedName, &scenario); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	name := runnerName(scenario.Name)
	if scenario.Spec.Suspend {
		var deployment appsv1.Deployment
		err := r.Get(ctx, types.NamespacedName{Namespace: scenario.Namespace, Name: name}, &deployment)
		if err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("get runner deployment: %w", err)
		}
		if err == nil {
			if err := r.Delete(ctx, &deployment); err != nil {
				return ctrl.Result{}, fmt.Errorf("delete suspended runner deployment: %w", err)
			}
		}
		return r.succeeded(ctx, &scenario, trafficv1alpha1.PhaseSuspended)
	}
	if err := validateScenario(&scenario); err != nil {
		return r.failed(ctx, &scenario, err)
	}
	if r.RunnerImage == "" {
		return r.failed(ctx, &scenario, fmt.Errorf("controller is missing KURAMA_RUNNER_IMAGE"))
	}
	if requiresRedis(&scenario) && r.RedisAddress == "" {
		return r.failed(ctx, &scenario, fmt.Errorf("controller is missing %s", runner.RedisAddressEnv))
	}

	configMap := desiredConfigMap(&scenario, name)
	if err := controllerutil.SetControllerReference(&scenario, configMap, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("set ConfigMap owner: %w", err)
	}
	if err := r.applyConfigMap(ctx, configMap); err != nil {
		return r.failed(ctx, &scenario, err)
	}

	deployment := desiredDeployment(&scenario, name, r.RunnerImage, r.RunnerImagePullSecret, r.RedisAddress)
	if err := controllerutil.SetControllerReference(&scenario, deployment, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("set Deployment owner: %w", err)
	}
	if err := r.applyDeployment(ctx, deployment); err != nil {
		return r.failed(ctx, &scenario, err)
	}

	return r.succeeded(ctx, &scenario, trafficv1alpha1.PhaseReady)
}

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
			return fmt.Errorf("spec.rate.limiter.type %q is unsupported; use local or redis", scenario.Spec.Rate.Limiter.Type)
		}
	}
	if scenario.Spec.Rate.Profile != nil {
		switch scenario.Spec.Rate.Profile.Type {
		case "", trafficv1alpha1.RateProfileTypeFixed, trafficv1alpha1.RateProfileTypeUniform:
		default:
			return fmt.Errorf("spec.rate.profile.type %q is unsupported; use fixed or uniform", scenario.Spec.Rate.Profile.Type)
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

func desiredConfigMap(scenario *trafficv1alpha1.TrafficScenario, name string) *corev1.ConfigMap {
	config := scenarioConfigJSON(scenario)
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: scenario.Namespace, Name: name, Labels: labels(scenario)},
		Data:       map[string]string{"scenario.json": config},
	}
}

func desiredDeployment(scenario *trafficv1alpha1.TrafficScenario, name, image, imagePullSecret, redisAddress string) *appsv1.Deployment {
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
		Volumes: []corev1.Volume{{Name: "scenario", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: name}}}}},
	}
	if imagePullSecret != "" {
		podSpec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: imagePullSecret}}
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: scenario.Namespace, Name: name, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(runnerReplicas(scenario)),
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

func runnerReplicas(scenario *trafficv1alpha1.TrafficScenario) int32 {
	if scenario.Spec.Replicas == 0 {
		return 1
	}
	return scenario.Spec.Replicas
}

func rateLimiterBackend(scenario *trafficv1alpha1.TrafficScenario) string {
	if scenario.Spec.Rate.Limiter != nil && scenario.Spec.Rate.Limiter.Type != "" {
		return string(scenario.Spec.Rate.Limiter.Type)
	}
	if storageBackend(scenario) == string(trafficv1alpha1.StorageTypeRedis) {
		return string(trafficv1alpha1.RateLimiterTypeRedis)
	}
	return string(trafficv1alpha1.RateLimiterTypeLocal)
}

func rateProfileType(scenario *trafficv1alpha1.TrafficScenario) string {
	if scenario.Spec.Rate.Profile == nil || scenario.Spec.Rate.Profile.Type == "" {
		return string(trafficv1alpha1.RateProfileTypeFixed)
	}
	return string(scenario.Spec.Rate.Profile.Type)
}

func requiresRedis(scenario *trafficv1alpha1.TrafficScenario) bool {
	return storageBackend(scenario) == string(trafficv1alpha1.StorageTypeRedis) ||
		rateLimiterBackend(scenario) == string(trafficv1alpha1.RateLimiterTypeRedis) ||
		scenario.Spec.Rate.Schedule.Type == trafficv1alpha1.RateScheduleTypeUniform
}

func storageBackend(scenario *trafficv1alpha1.TrafficScenario) string {
	if scenario.Spec.Storage == nil || scenario.Spec.Storage.Type == "" {
		return string(trafficv1alpha1.StorageTypeMemory)
	}
	return string(scenario.Spec.Storage.Type)
}

func scenarioRunnerConfig(scenario *trafficv1alpha1.TrafficScenario) runner.Config {
	config := runner.Config{
		Target: runner.TargetConfig{BaseURL: scenario.Spec.Target.BaseURL},
		Rate: runner.RateConfig{
			Schedule: runner.RateScheduleConfig{
				Type:                 string(scenario.Spec.Rate.Schedule.Type),
				RequestsPerMinute:    scenario.Spec.Rate.Schedule.RequestsPerMinute,
				MinRequestsPerMinute: scenario.Spec.Rate.Schedule.MinRequestsPerMinute,
				MaxRequestsPerMinute: scenario.Spec.Rate.Schedule.MaxRequestsPerMinute,
				WindowMinutes:        scenario.Spec.Rate.Schedule.WindowMinutes,
			},
			Limiter: &runner.RateLimiterConfig{
				Type: rateLimiterBackend(scenario),
			},
			Profile: &runner.RateProfileConfig{
				Type: rateProfileType(scenario),
			},
		},
		Stores:     make([]runner.StoreConfig, len(scenario.Spec.Stores)),
		Operations: make([]runner.OperationConfig, len(scenario.Spec.Operations)),
	}
	for i, store := range scenario.Spec.Stores {
		config.Stores[i] = runner.StoreConfig{Name: store.Name, Capacity: store.Capacity}
	}
	for i, operation := range scenario.Spec.Operations {
		converted := runner.OperationConfig{
			Name:   operation.Name,
			Weight: operation.Weight,
			Request: runner.RequestConfig{
				Method:       operation.Request.Method,
				PathTemplate: operation.Request.PathTemplate,
				Headers:      operation.Request.Headers,
				BodyTemplate: operation.Request.BodyTemplate,
				Variables:    make([]runner.VariableConfig, len(operation.Request.Variables)),
			},
			ExpectedStatusCodes: operation.ExpectedStatusCodes,
		}
		for j, variable := range operation.Request.Variables {
			converted.Request.Variables[j] = runner.VariableConfig{
				Name: variable.Name,
				Source: runner.VariableSource{
					Type:   variable.Source.Type,
					Store:  variable.Source.Store,
					Length: variable.Source.Length,
				},
			}
		}
		if operation.Capture != nil {
			converted.Capture = &runner.CaptureConfig{
				JSONPointer: operation.Capture.JSONPointer,
				Store:       operation.Capture.Store,
			}
		}
		config.Operations[i] = converted
	}
	return config
}

func scenarioConfigJSON(scenario *trafficv1alpha1.TrafficScenario) string {
	data, _ := json.Marshal(scenarioRunnerConfig(scenario))
	return string(data)
}

func configHash(config string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(config)))
}

func (r *TrafficScenarioReconciler) applyConfigMap(ctx context.Context, desired *corev1.ConfigMap) error {
	var existing corev1.ConfigMap
	key := client.ObjectKeyFromObject(desired)
	if err := r.Get(ctx, key, &existing); err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return fmt.Errorf("get runner ConfigMap: %w", err)
	}
	existing.Labels = desired.Labels
	existing.Data = desired.Data
	if err := r.Update(ctx, &existing); err != nil {
		return fmt.Errorf("update runner ConfigMap: %w", err)
	}
	return nil
}

func (r *TrafficScenarioReconciler) applyDeployment(ctx context.Context, desired *appsv1.Deployment) error {
	var existing appsv1.Deployment
	key := client.ObjectKeyFromObject(desired)
	if err := r.Get(ctx, key, &existing); err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return fmt.Errorf("get runner deployment: %w", err)
	}
	existing.Labels = desired.Labels
	existing.Spec = desired.Spec
	if err := r.Update(ctx, &existing); err != nil {
		return fmt.Errorf("update runner deployment: %w", err)
	}
	return nil
}

func (r *TrafficScenarioReconciler) succeeded(ctx context.Context, scenario *trafficv1alpha1.TrafficScenario, phase trafficv1alpha1.TrafficScenarioPhase) (ctrl.Result, error) {
	scenario.Status.Phase = phase
	scenario.Status.Message = ""
	scenario.Status.ObservedGeneration = scenario.Generation
	if err := r.Status().Update(ctx, scenario); err != nil {
		return ctrl.Result{}, fmt.Errorf("update TrafficScenario status: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *TrafficScenarioReconciler) failed(ctx context.Context, scenario *trafficv1alpha1.TrafficScenario, cause error) (ctrl.Result, error) {
	scenario.Status.Phase = trafficv1alpha1.PhaseFailed
	scenario.Status.Message = cause.Error()
	if err := r.Status().Update(ctx, scenario); err != nil {
		return ctrl.Result{}, fmt.Errorf("update failed TrafficScenario status: %w", err)
	}
	return ctrl.Result{}, cause
}

func (r *TrafficScenarioReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&trafficv1alpha1.TrafficScenario{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
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
