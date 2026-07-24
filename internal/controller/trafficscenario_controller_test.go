package controller

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
)

func TestReconcileCreatesRunnerResources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := newScheme(t)
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{Name: "shorturl", Namespace: "shorturl", Generation: 3},
		Spec:       validScenarioSpec(),
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(scenario).WithObjects(scenario).Build()
	reconciler := &TrafficScenarioReconciler{Client: client, Scheme: scheme, RunnerImage: "example.test/kurama:test", RunnerImagePullSecret: "registry-secret"}

	if _, err := reconciler.Reconcile(ctx, requestFor(scenario)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var configMap corev1.ConfigMap
	key := types.NamespacedName{Namespace: "shorturl", Name: "shorturl-runner"}
	if err := client.Get(ctx, key, &configMap); err != nil {
		t.Fatalf("get ConfigMap: %v", err)
	}
	config, err := runner.DecodeConfig(strings.NewReader(configMap.Data["scenario.json"]))
	if err != nil {
		t.Fatalf("decode scenario config: %v", err)
	}
	if config.Rate.Schedule.Type != "fixed" || config.Rate.Schedule.RequestsPerMinute != 30 || len(config.Stores) != 1 || len(config.Operations) != 3 {
		t.Fatalf("scenario config = %#v", config)
	}
	if config.Rate.Limiter == nil || config.Rate.Limiter.Type != "local" {
		t.Fatalf("scenario limiter config = %#v; want local", config.Rate.Limiter)
	}
	if config.Rate.Profile == nil || config.Rate.Profile.Type != "fixed" {
		t.Fatalf("scenario profile config = %#v; want fixed", config.Rate.Profile)
	}

	var deployment appsv1.Deployment
	if err := client.Get(ctx, key, &deployment); err != nil {
		t.Fatalf("get Deployment: %v", err)
	}
	container := deployment.Spec.Template.Spec.Containers[0]
	if got := container.Image; got != "example.test/kurama:test" {
		t.Fatalf("runner image = %q", got)
	}
	if got := deployment.Spec.Template.Spec.ImagePullSecrets; len(got) != 1 || got[0].Name != "registry-secret" {
		t.Fatalf("runner imagePullSecrets = %#v", got)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		t.Fatalf("runner replicas = %v; want 1", deployment.Spec.Replicas)
	}
	assertRunnerRolloutStrategy(t, deployment.Spec)
	if got := envValue(container.Env, runner.StoreBackendEnv); got != "memory" {
		t.Fatalf("runner store backend = %q, want memory", got)
	}
	if len(container.Ports) != 1 {
		t.Fatalf("runner ports = %#v, want one metrics port", container.Ports)
	}
	metricsPort := container.Ports[0]
	if metricsPort.Name != "metrics" || metricsPort.ContainerPort != 8080 || metricsPort.Protocol != corev1.ProtocolTCP {
		t.Fatalf("runner metrics port = %#v", metricsPort)
	}
	assertRunnerProbe(t, "startup", container.StartupProbe, runner.HealthPath, 2, 30)
	assertRunnerProbe(t, "liveness", container.LivenessProbe, runner.HealthPath, 10, 3)
	assertRunnerProbe(t, "readiness", container.ReadinessProbe, runner.ReadinessPath, 5, 3)
	annotations := deployment.Spec.Template.Annotations
	wantAnnotations := map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/port":   "8080",
		"prometheus.io/path":   "/metrics",
	}
	for name, want := range wantAnnotations {
		if got := annotations[name]; got != want {
			t.Errorf("runner annotation %q = %q, want %q", name, got, want)
		}
	}
	if got := deployment.Spec.Template.Annotations[configHashAnnotation]; got == "" {
		t.Fatal("runner config hash annotation is empty")
	}
}

func TestReconcileDoesNotWriteUnchangedResources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := newScheme(t)
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{
			Name: "shorturl", Namespace: "shorturl", UID: types.UID("scenario-uid"), Generation: 3,
		},
		Spec: validScenarioSpec(),
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(scenario).
		WithObjects(scenario).
		Build()
	reconciler := &TrafficScenarioReconciler{
		Client: fakeClient, Scheme: scheme, RunnerImage: "example.test/kurama:test",
	}

	if _, err := reconciler.Reconcile(ctx, requestFor(scenario)); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	key := types.NamespacedName{Namespace: scenario.Namespace, Name: "shorturl-runner"}
	var defaultedDeployment appsv1.Deployment
	if err := fakeClient.Get(ctx, key, &defaultedDeployment); err != nil {
		t.Fatalf("get Deployment for API defaults: %v", err)
	}
	defaultMode := int32(0o644)
	defaultedDeployment.Spec.Template.Spec.Volumes[0].ConfigMap.DefaultMode = &defaultMode
	defaultedDeployment.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullIfNotPresent
	defaultedDeployment.Spec.Template.Spec.Containers[0].TerminationMessagePath = "/dev/termination-log"
	defaultedDeployment.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyAlways
	defaultedDeployment.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirst
	if err := fakeClient.Update(ctx, &defaultedDeployment); err != nil {
		t.Fatalf("apply simulated API defaults: %v", err)
	}

	var configMapBefore corev1.ConfigMap
	var deploymentBefore appsv1.Deployment
	var scenarioBefore trafficv1alpha1.TrafficScenario
	if err := fakeClient.Get(ctx, key, &configMapBefore); err != nil {
		t.Fatalf("get ConfigMap before second reconcile: %v", err)
	}
	if err := fakeClient.Get(ctx, key, &deploymentBefore); err != nil {
		t.Fatalf("get Deployment before second reconcile: %v", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(scenario), &scenarioBefore); err != nil {
		t.Fatalf("get TrafficScenario before second reconcile: %v", err)
	}

	if _, err := reconciler.Reconcile(ctx, requestFor(scenario)); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	var configMapAfter corev1.ConfigMap
	var deploymentAfter appsv1.Deployment
	var scenarioAfter trafficv1alpha1.TrafficScenario
	if err := fakeClient.Get(ctx, key, &configMapAfter); err != nil {
		t.Fatalf("get ConfigMap after second reconcile: %v", err)
	}
	if err := fakeClient.Get(ctx, key, &deploymentAfter); err != nil {
		t.Fatalf("get Deployment after second reconcile: %v", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(scenario), &scenarioAfter); err != nil {
		t.Fatalf("get TrafficScenario after second reconcile: %v", err)
	}
	if configMapAfter.ResourceVersion != configMapBefore.ResourceVersion {
		t.Errorf(
			"ConfigMap resourceVersion changed from %q to %q",
			configMapBefore.ResourceVersion,
			configMapAfter.ResourceVersion,
		)
	}
	if deploymentAfter.ResourceVersion != deploymentBefore.ResourceVersion {
		t.Errorf(
			"Deployment resourceVersion changed from %q to %q",
			deploymentBefore.ResourceVersion,
			deploymentAfter.ResourceVersion,
		)
	}
	if scenarioAfter.ResourceVersion != scenarioBefore.ResourceVersion {
		t.Errorf(
			"TrafficScenario resourceVersion changed from %q to %q",
			scenarioBefore.ResourceVersion,
			scenarioAfter.ResourceVersion,
		)
	}
}

func TestReconcilePreservesUnmanagedResourceFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := newScheme(t)
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{
			Name: "shorturl", Namespace: "shorturl", UID: types.UID("scenario-uid"), Generation: 1,
		},
		Spec: validScenarioSpec(),
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(scenario).
		WithObjects(scenario).
		Build()
	reconciler := &TrafficScenarioReconciler{
		Client: fakeClient, Scheme: scheme, RunnerImage: "example.test/kurama:first",
	}
	if _, err := reconciler.Reconcile(ctx, requestFor(scenario)); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	key := types.NamespacedName{Namespace: scenario.Namespace, Name: "shorturl-runner"}
	var configMap corev1.ConfigMap
	if err := fakeClient.Get(ctx, key, &configMap); err != nil {
		t.Fatalf("get ConfigMap: %v", err)
	}
	configMap.Labels["example.test/custom"] = "preserved"
	configMap.Data["custom"] = "preserved"
	if err := fakeClient.Update(ctx, &configMap); err != nil {
		t.Fatalf("update ConfigMap: %v", err)
	}

	var deployment appsv1.Deployment
	if err := fakeClient.Get(ctx, key, &deployment); err != nil {
		t.Fatalf("get Deployment: %v", err)
	}
	deployment.Labels["example.test/custom"] = "preserved"
	deployment.Annotations = map[string]string{"example.test/custom": "preserved"}
	deployment.Spec.Template.Annotations["example.test/custom"] = "preserved"
	deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullAlways
	deployment.Spec.Template.Spec.Containers = append(
		deployment.Spec.Template.Spec.Containers,
		corev1.Container{Name: "injected", Image: "example.test/sidecar:test"},
	)
	if err := fakeClient.Update(ctx, &deployment); err != nil {
		t.Fatalf("update Deployment: %v", err)
	}

	var updatedScenario trafficv1alpha1.TrafficScenario
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(scenario), &updatedScenario); err != nil {
		t.Fatalf("get TrafficScenario: %v", err)
	}
	updatedScenario.Spec.Operations[0].Weight++
	updatedScenario.Generation++
	if err := fakeClient.Update(ctx, &updatedScenario); err != nil {
		t.Fatalf("update TrafficScenario: %v", err)
	}
	reconciler.RunnerImage = "example.test/kurama:second"
	if _, err := reconciler.Reconcile(ctx, requestFor(&updatedScenario)); err != nil {
		t.Fatalf("reconcile changed scenario: %v", err)
	}

	if err := fakeClient.Get(ctx, key, &configMap); err != nil {
		t.Fatalf("get reconciled ConfigMap: %v", err)
	}
	if configMap.Labels["example.test/custom"] != "preserved" || configMap.Data["custom"] != "preserved" {
		t.Fatalf("unmanaged ConfigMap fields were not preserved: %#v", configMap)
	}
	if err := fakeClient.Get(ctx, key, &deployment); err != nil {
		t.Fatalf("get reconciled Deployment: %v", err)
	}
	if deployment.Labels["example.test/custom"] != "preserved" ||
		deployment.Annotations["example.test/custom"] != "preserved" ||
		deployment.Spec.Template.Annotations["example.test/custom"] != "preserved" {
		t.Fatalf("unmanaged Deployment metadata was not preserved: %#v", deployment.ObjectMeta)
	}
	runnerContainer := deployment.Spec.Template.Spec.Containers[0]
	if runnerContainer.Image != "example.test/kurama:second" ||
		runnerContainer.ImagePullPolicy != corev1.PullAlways {
		t.Fatalf("runner container = %#v", runnerContainer)
	}
	if len(deployment.Spec.Template.Spec.Containers) != 2 ||
		deployment.Spec.Template.Spec.Containers[1].Name != "injected" {
		t.Fatalf("injected sidecar was not preserved: %#v", deployment.Spec.Template.Spec.Containers)
	}
}

func TestReconcileRefusesToModifyForeignOwnedDeployment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := newScheme(t)
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{
			Name: "shorturl", Namespace: "shorturl", UID: types.UID("scenario-uid"), Generation: 1,
		},
		Spec: validScenarioSpec(),
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(scenario).
		WithObjects(scenario).
		Build()
	reconciler := &TrafficScenarioReconciler{
		Client: fakeClient, Scheme: scheme, RunnerImage: "example.test/kurama:first",
	}
	if _, err := reconciler.Reconcile(ctx, requestFor(scenario)); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	key := types.NamespacedName{Namespace: scenario.Namespace, Name: "shorturl-runner"}
	var deployment appsv1.Deployment
	if err := fakeClient.Get(ctx, key, &deployment); err != nil {
		t.Fatalf("get Deployment: %v", err)
	}
	deployment.Spec.Template.Spec.Containers[0].Image = "example.test/foreign:keep"
	deployment.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: "example.test/v1",
		Kind:       "ForeignOwner",
		Name:       "foreign",
		UID:        types.UID("foreign-uid"),
		Controller: ptr.To(true),
	}}
	if err := fakeClient.Update(ctx, &deployment); err != nil {
		t.Fatalf("replace Deployment owner: %v", err)
	}

	reconciler.RunnerImage = "example.test/kurama:second"
	if _, err := reconciler.Reconcile(ctx, requestFor(scenario)); err == nil {
		t.Fatal("reconcile foreign-owned Deployment error = nil")
	}
	if err := fakeClient.Get(ctx, key, &deployment); err != nil {
		t.Fatalf("get Deployment after rejected reconcile: %v", err)
	}
	if got := deployment.Spec.Template.Spec.Containers[0].Image; got != "example.test/foreign:keep" {
		t.Fatalf("foreign-owned Deployment image = %q", got)
	}
}

func TestReconcileRefusesToAdoptExistingConfigMap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := newScheme(t)
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{
			Name: "shorturl", Namespace: "shorturl", UID: types.UID("scenario-uid"), Generation: 1,
		},
		Spec: validScenarioSpec(),
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "shorturl-runner", Namespace: scenario.Namespace},
		Data:       map[string]string{scenarioConfigKey: "foreign-data"},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(scenario).
		WithObjects(scenario, configMap).
		Build()
	reconciler := &TrafficScenarioReconciler{
		Client: fakeClient, Scheme: scheme, RunnerImage: "example.test/kurama:test",
	}

	if _, err := reconciler.Reconcile(ctx, requestFor(scenario)); err == nil {
		t.Fatal("reconcile existing ConfigMap error = nil")
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap); err != nil {
		t.Fatalf("get ConfigMap after rejected reconcile: %v", err)
	}
	if got := configMap.Data[scenarioConfigKey]; got != "foreign-data" {
		t.Fatalf("unowned ConfigMap data = %q", got)
	}
}

func assertRunnerRolloutStrategy(t *testing.T, spec appsv1.DeploymentSpec) {
	t.Helper()
	if spec.RevisionHistoryLimit == nil || *spec.RevisionHistoryLimit != runnerRevisionHistoryLimit {
		t.Errorf("revisionHistoryLimit = %v, want %d", spec.RevisionHistoryLimit, runnerRevisionHistoryLimit)
	}
	if spec.ProgressDeadlineSeconds == nil || *spec.ProgressDeadlineSeconds != runnerProgressDeadlineSeconds {
		t.Errorf("progressDeadlineSeconds = %v, want %d", spec.ProgressDeadlineSeconds, runnerProgressDeadlineSeconds)
	}
	if spec.Strategy.Type != appsv1.RollingUpdateDeploymentStrategyType || spec.Strategy.RollingUpdate == nil {
		t.Fatalf("runner strategy = %#v; want RollingUpdate", spec.Strategy)
	}
	rollingUpdate := spec.Strategy.RollingUpdate
	if rollingUpdate.MaxUnavailable == nil || rollingUpdate.MaxUnavailable.IntVal != 0 {
		t.Errorf("maxUnavailable = %v, want 0", rollingUpdate.MaxUnavailable)
	}
	if rollingUpdate.MaxSurge == nil || rollingUpdate.MaxSurge.IntVal != 1 {
		t.Errorf("maxSurge = %v, want 1", rollingUpdate.MaxSurge)
	}
}

func assertRunnerProbe(
	t *testing.T,
	name string,
	probe *corev1.Probe,
	path string,
	periodSeconds int32,
	failureThreshold int32,
) {
	t.Helper()
	if probe == nil || probe.HTTPGet == nil {
		t.Fatalf("%s probe = %#v; want HTTP probe", name, probe)
	}
	if probe.HTTPGet.Path != path || probe.HTTPGet.Port.StrVal != runner.MetricsPortName {
		t.Errorf("%s HTTP probe = %#v", name, probe.HTTPGet)
	}
	if probe.TimeoutSeconds != 1 || probe.PeriodSeconds != periodSeconds ||
		probe.SuccessThreshold != 1 || probe.FailureThreshold != failureThreshold {
		t.Errorf("%s probe timing = %#v", name, probe)
	}
}

func TestReconcileCreatesReplicatedRunnerWithRedisLimiterAndMemoryStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := newScheme(t)
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{Name: "shorturl", Namespace: "shorturl"},
		Spec:       validScenarioSpec(),
	}
	scenario.Spec.Replicas = 2
	scenario.Spec.Rate.Limiter = &trafficv1alpha1.RateLimiterSpec{Type: trafficv1alpha1.RateLimiterTypeRedis}
	scenario.Spec.Rate.Profile = &trafficv1alpha1.RateProfileSpec{Type: trafficv1alpha1.RateProfileTypeUniform}
	scenario.Spec.Rate.Schedule = trafficv1alpha1.RateScheduleSpec{
		Type: trafficv1alpha1.RateScheduleTypeUniform, MinRequestsPerMinute: 2, MaxRequestsPerMinute: 56, WindowMinutes: 1,
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(scenario).WithObjects(scenario).Build()
	reconciler := &TrafficScenarioReconciler{
		Client: client, Scheme: scheme, RunnerImage: "example.test/kurama:test", RedisAddress: "kurama-redis:6379",
	}

	if _, err := reconciler.Reconcile(ctx, requestFor(scenario)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	key := types.NamespacedName{Namespace: "shorturl", Name: "shorturl-runner"}
	var deployment appsv1.Deployment
	if err := client.Get(ctx, key, &deployment); err != nil {
		t.Fatalf("get Deployment: %v", err)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 2 {
		t.Fatalf("runner replicas = %v; want 2", deployment.Spec.Replicas)
	}
	environment := deployment.Spec.Template.Spec.Containers[0].Env
	if got := envValue(environment, runner.StoreBackendEnv); got != "memory" {
		t.Fatalf("runner store backend = %q; want memory", got)
	}
	if got := envValue(environment, runner.RedisAddressEnv); got != "kurama-redis:6379" {
		t.Fatalf("runner Redis address = %q", got)
	}

	var configMap corev1.ConfigMap
	if err := client.Get(ctx, key, &configMap); err != nil {
		t.Fatalf("get ConfigMap: %v", err)
	}
	config, err := runner.DecodeConfig(strings.NewReader(configMap.Data["scenario.json"]))
	if err != nil {
		t.Fatalf("decode scenario config: %v", err)
	}
	if config.Rate.Limiter == nil || config.Rate.Limiter.Type != "redis" {
		t.Fatalf("scenario limiter config = %#v; want redis", config.Rate.Limiter)
	}
	if config.Rate.Profile == nil || config.Rate.Profile.Type != "uniform" {
		t.Fatalf("scenario profile config = %#v; want uniform", config.Rate.Profile)
	}
	if config.Rate.Schedule.Type != "uniform" || config.Rate.Schedule.MinRequestsPerMinute != 2 ||
		config.Rate.Schedule.MaxRequestsPerMinute != 56 || config.Rate.Schedule.WindowMinutes != 1 {
		t.Fatalf("scenario schedule config = %#v", config.Rate.Schedule)
	}
}

func TestReconcileCreatesRedisRunnerEnvironment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := newScheme(t)
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{Name: "shorturl", Namespace: "shorturl"},
		Spec:       validScenarioSpec(),
	}
	scenario.Spec.Storage = &trafficv1alpha1.StorageSpec{Type: trafficv1alpha1.StorageTypeRedis}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(scenario).WithObjects(scenario).Build()
	reconciler := &TrafficScenarioReconciler{
		Client:       client,
		Scheme:       scheme,
		RunnerImage:  "example.test/kurama:test",
		RedisAddress: "kurama-redis:6379",
	}

	if _, err := reconciler.Reconcile(ctx, requestFor(scenario)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var deployment appsv1.Deployment
	key := types.NamespacedName{Namespace: "shorturl", Name: "shorturl-runner"}
	if err := client.Get(ctx, key, &deployment); err != nil {
		t.Fatalf("get Deployment: %v", err)
	}
	environment := deployment.Spec.Template.Spec.Containers[0].Env
	if got := envValue(environment, runner.StoreBackendEnv); got != "redis" {
		t.Fatalf("runner store backend = %q, want redis", got)
	}
	if got := envValue(environment, runner.RedisAddressEnv); got != "kurama-redis:6379" {
		t.Fatalf("runner Redis address = %q", got)
	}
	if got := envValue(environment, runner.ScenarioEnv); got != "shorturl" {
		t.Fatalf("runner scenario = %q", got)
	}
	namespace := envVar(environment, runner.NamespaceEnv)
	if namespace == nil || namespace.ValueFrom == nil || namespace.ValueFrom.FieldRef == nil || namespace.ValueFrom.FieldRef.FieldPath != "metadata.namespace" {
		t.Fatalf("runner namespace env = %#v", namespace)
	}
}

func TestReconcileRejectsRedisWithoutControllerAddress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := newScheme(t)
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{Name: "shorturl", Namespace: "shorturl"},
		Spec:       validScenarioSpec(),
	}
	scenario.Spec.Storage = &trafficv1alpha1.StorageSpec{Type: trafficv1alpha1.StorageTypeRedis}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(scenario).WithObjects(scenario).Build()
	reconciler := &TrafficScenarioReconciler{Client: client, Scheme: scheme, RunnerImage: "example.test/kurama:test"}

	if _, err := reconciler.Reconcile(ctx, requestFor(scenario)); err == nil {
		t.Fatal("reconcile error = nil")
	}
	var actual trafficv1alpha1.TrafficScenario
	if err := client.Get(ctx, types.NamespacedName{Namespace: "shorturl", Name: "shorturl"}, &actual); err != nil {
		t.Fatalf("get TrafficScenario: %v", err)
	}
	if actual.Status.Phase != trafficv1alpha1.PhaseFailed || !strings.Contains(actual.Status.Message, runner.RedisAddressEnv) {
		t.Fatalf("scenario status = %#v", actual.Status)
	}
}

func TestReconcileRejectsRedisLimiterWithoutControllerAddress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := newScheme(t)
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{Name: "shorturl", Namespace: "shorturl"},
		Spec:       validScenarioSpec(),
	}
	scenario.Spec.Rate.Limiter = &trafficv1alpha1.RateLimiterSpec{Type: trafficv1alpha1.RateLimiterTypeRedis}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(scenario).WithObjects(scenario).Build()
	reconciler := &TrafficScenarioReconciler{Client: client, Scheme: scheme, RunnerImage: "example.test/kurama:test"}

	if _, err := reconciler.Reconcile(ctx, requestFor(scenario)); err == nil {
		t.Fatal("reconcile error = nil")
	}
	var actual trafficv1alpha1.TrafficScenario
	if err := client.Get(ctx, types.NamespacedName{Namespace: "shorturl", Name: "shorturl"}, &actual); err != nil {
		t.Fatalf("get TrafficScenario: %v", err)
	}
	if actual.Status.Phase != trafficv1alpha1.PhaseFailed || !strings.Contains(actual.Status.Message, runner.RedisAddressEnv) {
		t.Fatalf("scenario status = %#v", actual.Status)
	}
}

func TestReconcileRejectsUniformScheduleWithoutControllerAddress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := newScheme(t)
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{Name: "shorturl", Namespace: "shorturl"},
		Spec:       validScenarioSpec(),
	}
	scenario.Spec.Rate.Schedule = trafficv1alpha1.RateScheduleSpec{
		Type: trafficv1alpha1.RateScheduleTypeUniform, MinRequestsPerMinute: 2, MaxRequestsPerMinute: 56, WindowMinutes: 1,
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(scenario).WithObjects(scenario).Build()
	reconciler := &TrafficScenarioReconciler{Client: client, Scheme: scheme, RunnerImage: "example.test/kurama:test"}

	if _, err := reconciler.Reconcile(ctx, requestFor(scenario)); err == nil {
		t.Fatal("reconcile error = nil")
	}
	var actual trafficv1alpha1.TrafficScenario
	if err := client.Get(ctx, types.NamespacedName{Namespace: "shorturl", Name: "shorturl"}, &actual); err != nil {
		t.Fatalf("get TrafficScenario: %v", err)
	}
	if actual.Status.Phase != trafficv1alpha1.PhaseFailed || !strings.Contains(actual.Status.Message, runner.RedisAddressEnv) {
		t.Fatalf("scenario status = %#v", actual.Status)
	}
}

func TestReconcileSuspendDeletesRunnerDeployment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := newScheme(t)
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{Name: "shorturl", Namespace: "shorturl"},
		Spec:       trafficv1alpha1.TrafficScenarioSpec{Suspend: true},
	}
	deployment := desiredDeployment(scenario, "shorturl-runner", "example.test/kurama:test", "", "")
	scenario.UID = types.UID("scenario-uid")
	if err := controllerutil.SetControllerReference(scenario, deployment, scheme); err != nil {
		t.Fatalf("set Deployment owner: %v", err)
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(scenario).WithObjects(scenario, deployment).Build()
	reconciler := &TrafficScenarioReconciler{Client: client, Scheme: scheme, RunnerImage: "example.test/kurama:test"}

	if _, err := reconciler.Reconcile(ctx, requestFor(scenario)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var actual appsv1.Deployment
	if err := client.Get(ctx, types.NamespacedName{Namespace: scenario.Namespace, Name: "shorturl-runner"}, &actual); err == nil {
		t.Fatal("runner Deployment still exists after suspension")
	}
}

func TestValidateScenarioRejectsNonHTTPURL(t *testing.T) {
	t.Parallel()
	scenario := &trafficv1alpha1.TrafficScenario{Spec: validScenarioSpec()}
	scenario.Spec.Target.BaseURL = "postgres://db"
	if err := validateScenario(scenario); err == nil {
		t.Fatal("validateScenario unexpectedly accepted postgres URL")
	}
}

func TestValidateScenarioRejectsUnknownStorageType(t *testing.T) {
	t.Parallel()
	scenario := &trafficv1alpha1.TrafficScenario{Spec: validScenarioSpec()}
	scenario.Spec.Storage = &trafficv1alpha1.StorageSpec{Type: "postgres"}
	if err := validateScenario(scenario); err == nil {
		t.Fatal("validateScenario unexpectedly accepted unknown storage type")
	}
}

func TestValidateScenarioRejectsUnknownLimiterAndLocalMultiReplica(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*trafficv1alpha1.TrafficScenario)
	}{
		{name: "unknown limiter", mutate: func(scenario *trafficv1alpha1.TrafficScenario) {
			scenario.Spec.Rate.Limiter = &trafficv1alpha1.RateLimiterSpec{Type: "postgres"}
		}},
		{name: "local multi replica", mutate: func(scenario *trafficv1alpha1.TrafficScenario) {
			scenario.Spec.Replicas = 2
			scenario.Spec.Rate.Limiter = &trafficv1alpha1.RateLimiterSpec{Type: trafficv1alpha1.RateLimiterTypeLocal}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			scenario := &trafficv1alpha1.TrafficScenario{Spec: validScenarioSpec()}
			test.mutate(scenario)
			if err := validateScenario(scenario); err == nil {
				t.Fatal("validateScenario() error = nil")
			}
		})
	}
}

func TestValidateScenarioRejectsUnknownRateProfile(t *testing.T) {
	t.Parallel()
	scenario := &trafficv1alpha1.TrafficScenario{Spec: validScenarioSpec()}
	scenario.Spec.Rate.Profile = &trafficv1alpha1.RateProfileSpec{Type: "normal"}
	if err := validateScenario(scenario); err == nil {
		t.Fatal("validateScenario() error = nil")
	}
}

func TestScenarioRunnerConfigCopiesBurstProfile(t *testing.T) {
	t.Parallel()
	scenario := &trafficv1alpha1.TrafficScenario{Spec: validScenarioSpec()}
	scenario.Spec.Rate.Profile = &trafficv1alpha1.RateProfileSpec{
		Type:         trafficv1alpha1.RateProfileTypeBurst,
		MinBurstSize: 5,
		MaxBurstSize: 15,
		DelayDivisor: 10,
	}

	if err := validateScenario(scenario); err != nil {
		t.Fatalf("validateScenario() error = %v", err)
	}
	profile := scenarioRunnerConfig(scenario).Rate.Profile
	if profile == nil || profile.Type != "burst" ||
		profile.MinBurstSize != 5 || profile.MaxBurstSize != 15 || profile.DelayDivisor != 10 {
		t.Fatalf("runner burst profile = %#v", profile)
	}
}

func TestValidateScenarioRejectsInvalidBurstProfile(t *testing.T) {
	t.Parallel()
	scenario := &trafficv1alpha1.TrafficScenario{Spec: validScenarioSpec()}
	scenario.Spec.Rate.Profile = &trafficv1alpha1.RateProfileSpec{
		Type:         trafficv1alpha1.RateProfileTypeBurst,
		MinBurstSize: 15,
		MaxBurstSize: 5,
	}
	if err := validateScenario(scenario); err == nil {
		t.Fatal("validateScenario() error = nil")
	}
}

func TestValidateScenarioRejectsUnknownRateSchedule(t *testing.T) {
	t.Parallel()
	scenario := &trafficv1alpha1.TrafficScenario{Spec: validScenarioSpec()}
	scenario.Spec.Rate.Schedule.Type = "burst"
	if err := validateScenario(scenario); err == nil {
		t.Fatal("validateScenario() error = nil")
	}
}

func TestDesiredDeploymentConfigChangeUpdatesHash(t *testing.T) {
	t.Parallel()
	scenario := &trafficv1alpha1.TrafficScenario{Spec: validScenarioSpec()}
	before := desiredDeployment(scenario, "shorturl-runner", "image", "", "").Spec.Template.Annotations[configHashAnnotation]
	scenario.Spec.Operations[0].Weight++
	after := desiredDeployment(scenario, "shorturl-runner", "image", "", "").Spec.Template.Annotations[configHashAnnotation]
	if before == after {
		t.Fatal("config hash did not change after scenario update")
	}
}

func validScenarioSpec() trafficv1alpha1.TrafficScenarioSpec {
	return trafficv1alpha1.TrafficScenarioSpec{
		Target: trafficv1alpha1.TargetSpec{BaseURL: "http://shorturl.shorturl.svc.cluster.local"},
		Rate: trafficv1alpha1.RateSpec{Schedule: trafficv1alpha1.RateScheduleSpec{
			Type: trafficv1alpha1.RateScheduleTypeFixed, RequestsPerMinute: 30,
		}},
		Stores: []trafficv1alpha1.StoreSpec{{Name: "hashes", Capacity: 10_000}},
		Operations: []trafficv1alpha1.OperationSpec{
			{
				Name: "create", Weight: 20,
				Request: trafficv1alpha1.RequestSpec{
					Method: "POST", PathTemplate: "/api/v1/data/shorten",
					Headers:      map[string]string{"Content-Type": "application/json"},
					BodyTemplate: `{"longURL":"https://example.invalid/kurama/{{id}}"}`,
					Variables:    []trafficv1alpha1.VariableSpec{{Name: "id", Source: trafficv1alpha1.VariableSourceSpec{Type: "randomUUID"}}},
				},
				ExpectedStatusCodes: []int{200},
				Capture:             &trafficv1alpha1.CaptureSpec{JSONPointer: "/shortURL", Store: "hashes"},
			},
			{
				Name: "resolve-valid", Weight: 70,
				Request: trafficv1alpha1.RequestSpec{
					Method: "GET", PathTemplate: "/api/v1/{{hash}}",
					Variables: []trafficv1alpha1.VariableSpec{{Name: "hash", Source: trafficv1alpha1.VariableSourceSpec{Type: "store", Store: "hashes"}}},
				},
				ExpectedStatusCodes: []int{308},
			},
			{
				Name: "resolve-invalid", Weight: 10,
				Request: trafficv1alpha1.RequestSpec{
					Method: "GET", PathTemplate: "/api/v1/{{hash}}",
					Variables: []trafficv1alpha1.VariableSpec{{Name: "hash", Source: trafficv1alpha1.VariableSourceSpec{Type: "randomBase62", Length: 8}}},
				},
				ExpectedStatusCodes: []int{404},
			},
		},
	}
}

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := trafficv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func requestFor(scenario *trafficv1alpha1.TrafficScenario) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Namespace: scenario.Namespace, Name: scenario.Name}}
}

func envValue(environment []corev1.EnvVar, name string) string {
	variable := envVar(environment, name)
	if variable == nil {
		return ""
	}
	return variable.Value
}

func envVar(environment []corev1.EnvVar, name string) *corev1.EnvVar {
	for i := range environment {
		if environment[i].Name == name {
			return &environment[i]
		}
	}
	return nil
}
