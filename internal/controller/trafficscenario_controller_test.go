package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
)

func TestReconcileCreatesRunnerResources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := newScheme(t)
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{Name: "shorturl", Namespace: "shorturl", Generation: 3},
		Spec:       trafficv1alpha1.TrafficScenarioSpec{Target: trafficv1alpha1.TargetSpec{BaseURL: "http://shorturl.shorturl.svc.cluster.local"}},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(scenario).WithObjects(scenario).Build()
	reconciler := &TrafficScenarioReconciler{Client: client, Scheme: scheme, RunnerImage: "example.test/kurama:test"}

	if _, err := reconciler.Reconcile(ctx, requestFor(scenario)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var configMap corev1.ConfigMap
	key := types.NamespacedName{Namespace: "shorturl", Name: "shorturl-runner"}
	if err := client.Get(ctx, key, &configMap); err != nil {
		t.Fatalf("get ConfigMap: %v", err)
	}
	if got := configMap.Data["scenario.json"]; got != `{"target":{"baseURL":"http://shorturl.shorturl.svc.cluster.local"}}` {
		t.Fatalf("scenario config = %s", got)
	}

	var deployment appsv1.Deployment
	if err := client.Get(ctx, key, &deployment); err != nil {
		t.Fatalf("get Deployment: %v", err)
	}
	if got := deployment.Spec.Template.Spec.Containers[0].Image; got != "example.test/kurama:test" {
		t.Fatalf("runner image = %q", got)
	}
}

func TestReconcileSuspendDeletesRunnerDeployment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := newScheme(t)
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{Name: "shorturl", Namespace: "shorturl"},
		Spec:       trafficv1alpha1.TrafficScenarioSpec{Target: trafficv1alpha1.TargetSpec{BaseURL: "https://shorturl.example.test"}, Suspend: true},
	}
	deployment := desiredDeployment(scenario, "shorturl-runner", "example.test/kurama:test")
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
	scenario := &trafficv1alpha1.TrafficScenario{Spec: trafficv1alpha1.TrafficScenarioSpec{Target: trafficv1alpha1.TargetSpec{BaseURL: "postgres://db"}}}
	if err := validateScenario(scenario); err == nil {
		t.Fatal("validateScenario unexpectedly accepted postgres URL")
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
