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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
	if config.Rate.RequestsPerMinute != 30 || len(config.Stores) != 1 || len(config.Operations) != 3 {
		t.Fatalf("scenario config = %#v", config)
	}

	var deployment appsv1.Deployment
	if err := client.Get(ctx, key, &deployment); err != nil {
		t.Fatalf("get Deployment: %v", err)
	}
	if got := deployment.Spec.Template.Spec.Containers[0].Image; got != "example.test/kurama:test" {
		t.Fatalf("runner image = %q", got)
	}
	if got := deployment.Spec.Template.Spec.ImagePullSecrets; len(got) != 1 || got[0].Name != "registry-secret" {
		t.Fatalf("runner imagePullSecrets = %#v", got)
	}
	if got := deployment.Spec.Template.Annotations[configHashAnnotation]; got == "" {
		t.Fatal("runner config hash annotation is empty")
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
	deployment := desiredDeployment(scenario, "shorturl-runner", "example.test/kurama:test", "")
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

func TestDesiredDeploymentConfigChangeUpdatesHash(t *testing.T) {
	t.Parallel()
	scenario := &trafficv1alpha1.TrafficScenario{Spec: validScenarioSpec()}
	before := desiredDeployment(scenario, "shorturl-runner", "image", "").Spec.Template.Annotations[configHashAnnotation]
	scenario.Spec.Operations[0].Weight++
	after := desiredDeployment(scenario, "shorturl-runner", "image", "").Spec.Template.Annotations[configHashAnnotation]
	if before == after {
		t.Fatal("config hash did not change after scenario update")
	}
}

func validScenarioSpec() trafficv1alpha1.TrafficScenarioSpec {
	return trafficv1alpha1.TrafficScenarioSpec{
		Target: trafficv1alpha1.TargetSpec{BaseURL: "http://shorturl.shorturl.svc.cluster.local"},
		Rate:   trafficv1alpha1.RateSpec{RequestsPerMinute: 30},
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
