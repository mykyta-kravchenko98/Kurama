package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
)

func (r *TrafficScenarioReconciler) applyConfigMap(
	ctx context.Context,
	scenario *trafficv1alpha1.TrafficScenario,
	desired *corev1.ConfigMap,
) error {
	var existing corev1.ConfigMap
	key := client.ObjectKeyFromObject(desired)
	if err := r.Get(ctx, key, &existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get runner ConfigMap: %w", err)
		}
		if err := controllerutil.SetControllerReference(scenario, desired, r.Scheme); err != nil {
			return fmt.Errorf("set runner ConfigMap owner: %w", err)
		}
		if err := r.Create(ctx, desired); err != nil {
			return fmt.Errorf("create runner ConfigMap: %w", err)
		}
		return nil
	}
	if err := ensureControlledBy(&existing, scenario, "ConfigMap"); err != nil {
		return err
	}

	before := existing.DeepCopy()
	setManagedMapValues(&existing.Labels, desired.Labels)
	if existing.Data == nil {
		existing.Data = make(map[string]string, 1)
	}
	existing.Data[scenarioConfigKey] = desired.Data[scenarioConfigKey]
	if apiequality.Semantic.DeepEqual(before, &existing) {
		return nil
	}
	if err := r.Patch(ctx, &existing, client.MergeFrom(before)); err != nil {
		return fmt.Errorf("patch runner ConfigMap: %w", err)
	}
	return nil
}

func (r *TrafficScenarioReconciler) applyDeployment(
	ctx context.Context,
	scenario *trafficv1alpha1.TrafficScenario,
	desired *appsv1.Deployment,
) error {
	var existing appsv1.Deployment
	key := client.ObjectKeyFromObject(desired)
	if err := r.Get(ctx, key, &existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get runner Deployment: %w", err)
		}
		if err := controllerutil.SetControllerReference(scenario, desired, r.Scheme); err != nil {
			return fmt.Errorf("set runner Deployment owner: %w", err)
		}
		if err := r.Create(ctx, desired); err != nil {
			return fmt.Errorf("create runner Deployment: %w", err)
		}
		return nil
	}
	if err := ensureControlledBy(&existing, scenario, "Deployment"); err != nil {
		return err
	}
	if !apiequality.Semantic.DeepEqual(existing.Spec.Selector, desired.Spec.Selector) {
		return fmt.Errorf("runner Deployment %q has an unexpected immutable selector", existing.Name)
	}

	before := existing.DeepCopy()
	applyManagedDeploymentFields(&existing, desired)
	if apiequality.Semantic.DeepEqual(before, &existing) {
		return nil
	}
	if err := r.Patch(ctx, &existing, client.MergeFrom(before)); err != nil {
		return fmt.Errorf("patch runner Deployment: %w", err)
	}
	return nil
}

func applyManagedDeploymentFields(existing, desired *appsv1.Deployment) {
	setManagedMapValues(&existing.Labels, desired.Labels)
	existing.Spec.Replicas = desired.Spec.Replicas
	existing.Spec.RevisionHistoryLimit = desired.Spec.RevisionHistoryLimit
	existing.Spec.ProgressDeadlineSeconds = desired.Spec.ProgressDeadlineSeconds
	existing.Spec.Strategy = desired.Spec.Strategy

	setManagedMapValues(&existing.Spec.Template.Labels, desired.Spec.Template.Labels)
	setManagedMapValues(&existing.Spec.Template.Annotations, desired.Spec.Template.Annotations)
	existing.Spec.Template.Spec.ImagePullSecrets = desired.Spec.Template.Spec.ImagePullSecrets
	applyManagedRunnerContainer(&existing.Spec.Template.Spec, desired.Spec.Template.Spec.Containers[0])
	applyManagedScenarioVolume(&existing.Spec.Template.Spec, desired.Spec.Template.Spec.Volumes[0])
}

func applyManagedRunnerContainer(podSpec *corev1.PodSpec, desired corev1.Container) {
	for i := range podSpec.Containers {
		if podSpec.Containers[i].Name != desired.Name {
			continue
		}
		container := &podSpec.Containers[i]
		container.Image = desired.Image
		container.Command = desired.Command
		container.Args = desired.Args
		container.Env = desired.Env
		container.VolumeMounts = desired.VolumeMounts
		container.Ports = desired.Ports
		container.StartupProbe = desired.StartupProbe
		container.LivenessProbe = desired.LivenessProbe
		container.ReadinessProbe = desired.ReadinessProbe
		return
	}
	podSpec.Containers = append(podSpec.Containers, desired)
}

func applyManagedScenarioVolume(podSpec *corev1.PodSpec, desired corev1.Volume) {
	for i := range podSpec.Volumes {
		if podSpec.Volumes[i].Name != desired.Name {
			continue
		}
		if podSpec.Volumes[i].ConfigMap == nil || desired.ConfigMap == nil {
			podSpec.Volumes[i].VolumeSource = desired.VolumeSource
			return
		}
		// DefaultMode is defaulted by the API server. Preserve it while
		// reconciling only the ConfigMap fields owned by Kurama.
		podSpec.Volumes[i].ConfigMap.LocalObjectReference = desired.ConfigMap.LocalObjectReference
		podSpec.Volumes[i].ConfigMap.Items = desired.ConfigMap.Items
		podSpec.Volumes[i].ConfigMap.Optional = desired.ConfigMap.Optional
		return
	}
	podSpec.Volumes = append(podSpec.Volumes, desired)
}

func setManagedMapValues(target *map[string]string, desired map[string]string) {
	if *target == nil {
		*target = make(map[string]string, len(desired))
	}
	for key, value := range desired {
		(*target)[key] = value
	}
}

func ensureControlledBy(
	object metav1.Object,
	scenario *trafficv1alpha1.TrafficScenario,
	kind string,
) error {
	if metav1.IsControlledBy(object, scenario) {
		return nil
	}
	controller := metav1.GetControllerOf(object)
	if controller == nil {
		return fmt.Errorf(
			"runner %s %q already exists and is not controlled by TrafficScenario %q",
			kind,
			object.GetName(),
			scenario.Name,
		)
	}
	return fmt.Errorf(
		"runner %s %q is controlled by %s %q, not TrafficScenario %q",
		kind,
		object.GetName(),
		controller.Kind,
		controller.Name,
		scenario.Name,
	)
}
