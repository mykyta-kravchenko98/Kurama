package controller

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
)

func TestSecretHeaderUsesProjectedSecretWithoutLeakingReferenceIntoConfig(t *testing.T) {
	t.Parallel()
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{Name: "shorturl", Namespace: "shorturl"},
		Spec:       validScenarioSpec(),
	}
	scenario.Spec.Operations[0].Request.SecretHeaders = []trafficv1alpha1.SecretHeaderSpec{{
		Name: "Authorization",
		ValueFrom: trafficv1alpha1.SecretHeaderValueSource{SecretKeyRef: trafficv1alpha1.SecretKeySelector{
			Name: "shorturl-api-auth", Key: "authorization",
		}},
	}}

	config := mustScenarioConfigJSON(t, scenario)
	for _, forbidden := range []string{"shorturl-api-auth", `"key":"authorization"`} {
		if strings.Contains(config, forbidden) {
			t.Fatalf("runner config leaked Secret reference %q: %s", forbidden, config)
		}
	}
	if !strings.Contains(config, runner.SecretHeadersMountPath+"/operation-000-header-000") {
		t.Fatalf("runner config does not contain projected file path: %s", config)
	}

	deployment := desiredDeployment(scenario, "shorturl-runner", "image", "", "", config)
	podSpec := deployment.Spec.Template.Spec
	if len(podSpec.Containers[0].VolumeMounts) != 2 {
		t.Fatalf("runner volume mounts = %#v, want scenario and request-secrets", podSpec.Containers[0].VolumeMounts)
	}
	var found bool
	for _, volume := range podSpec.Volumes {
		if volume.Name != secretHeadersVolumeName {
			continue
		}
		found = true
		if volume.Projected == nil || len(volume.Projected.Sources) != 1 {
			t.Fatalf("secret volume = %#v", volume)
		}
		projection := volume.Projected.Sources[0].Secret
		if projection == nil || projection.Name != "shorturl-api-auth" ||
			len(projection.Items) != 1 || projection.Items[0].Key != "authorization" {
			t.Fatalf("secret projection = %#v", projection)
		}
	}
	if !found {
		t.Fatal("request-secrets projected volume is missing")
	}
}

func TestApplyManagedDeploymentRemovesObsoleteSecretVolume(t *testing.T) {
	t.Parallel()
	scenario := &trafficv1alpha1.TrafficScenario{Spec: validScenarioSpec()}
	scenario.Spec.Operations[0].Request.SecretHeaders = []trafficv1alpha1.SecretHeaderSpec{{
		Name: "Authorization",
		ValueFrom: trafficv1alpha1.SecretHeaderValueSource{SecretKeyRef: trafficv1alpha1.SecretKeySelector{
			Name: "api-auth", Key: "authorization",
		}},
	}}
	withSecretConfig := mustScenarioConfigJSON(t, scenario)
	existing := desiredDeployment(scenario, "runner", "image", "", "", withSecretConfig)

	scenario.Spec.Operations[0].Request.SecretHeaders = nil
	withoutSecretConfig := mustScenarioConfigJSON(t, scenario)
	desired := desiredDeployment(scenario, "runner", "image", "", "", withoutSecretConfig)
	applyManagedDeploymentFields(existing, desired)

	for _, volume := range existing.Spec.Template.Spec.Volumes {
		if volume.Name == secretHeadersVolumeName {
			t.Fatal("obsolete request-secrets volume was retained")
		}
	}
	for _, mount := range existing.Spec.Template.Spec.Containers[0].VolumeMounts {
		if mount.Name == secretHeadersVolumeName {
			t.Fatal("obsolete request-secrets mount was retained")
		}
	}
}

func TestValidateScenarioRejectsInvalidSecretReference(t *testing.T) {
	t.Parallel()
	scenario := &trafficv1alpha1.TrafficScenario{Spec: validScenarioSpec()}
	scenario.Spec.Operations[0].Request.SecretHeaders = []trafficv1alpha1.SecretHeaderSpec{{
		Name: "Authorization",
		ValueFrom: trafficv1alpha1.SecretHeaderValueSource{SecretKeyRef: trafficv1alpha1.SecretKeySelector{
			Name: "", Key: "authorization",
		}},
	}}
	if err := validateScenario(scenario); err == nil || !strings.Contains(err.Error(), "secretKeyRef.name") {
		t.Fatalf("validateScenario() error = %v, want invalid Secret name", err)
	}
}
