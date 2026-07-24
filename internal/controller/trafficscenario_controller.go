// Package controller reconciles TrafficScenario resources into runner Pods.
package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
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

func (r *TrafficScenarioReconciler) succeeded(
	ctx context.Context,
	scenario *trafficv1alpha1.TrafficScenario,
	phase trafficv1alpha1.TrafficScenarioPhase,
) (ctrl.Result, error) {
	scenario.Status.Phase = phase
	scenario.Status.Message = ""
	scenario.Status.ObservedGeneration = scenario.Generation
	if err := r.Status().Update(ctx, scenario); err != nil {
		return ctrl.Result{}, fmt.Errorf("update TrafficScenario status: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *TrafficScenarioReconciler) failed(
	ctx context.Context,
	scenario *trafficv1alpha1.TrafficScenario,
	cause error,
) (ctrl.Result, error) {
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
