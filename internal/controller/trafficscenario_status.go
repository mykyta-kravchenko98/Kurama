package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
)

const rolloutRequeueInterval = 5 * time.Second

const (
	reasonControllerConfigurationInvalid = "ControllerConfigurationInvalid"
	reasonReconcileFailed                = "ReconcileFailed"
	reasonRolloutFailed                  = "RolloutFailed"
	reasonRunnerDeletionRequested        = "RunnerDeletionRequested"
	reasonRunnerProgressing              = "RunnerProgressing"
	reasonRunnerReady                    = "RunnerReady"
	reasonScenarioSuspended              = "ScenarioSuspended"
	reasonValidationFailed               = "ValidationFailed"
)

type scenarioState struct {
	phase           trafficv1alpha1.TrafficScenarioPhase
	activeCondition string
	reason          string
	message         string
	requeueAfter    time.Duration
}

func progressingState(reason, message string) scenarioState {
	return scenarioState{
		phase:           trafficv1alpha1.PhaseProgressing,
		activeCondition: trafficv1alpha1.ConditionProgressing,
		reason:          reason,
		message:         message,
		requeueAfter:    rolloutRequeueInterval,
	}
}

func degradedState(
	phase trafficv1alpha1.TrafficScenarioPhase,
	reason string,
	message string,
) scenarioState {
	return scenarioState{
		phase:           phase,
		activeCondition: trafficv1alpha1.ConditionDegraded,
		reason:          reason,
		message:         message,
	}
}

func (r *TrafficScenarioReconciler) reconcileResultForState(
	ctx context.Context,
	scenario *trafficv1alpha1.TrafficScenario,
	state scenarioState,
) (ctrl.Result, error) {
	if err := r.updateScenarioStatus(ctx, scenario, state); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: state.requeueAfter}, nil
}

func (r *TrafficScenarioReconciler) updateScenarioStatus(
	ctx context.Context,
	scenario *trafficv1alpha1.TrafficScenario,
	state scenarioState,
) error {
	before := scenario.DeepCopy()
	scenario.Status.Phase = state.phase
	scenario.Status.Message = state.message
	scenario.Status.ObservedGeneration = scenario.Generation

	for _, conditionType := range []string{
		trafficv1alpha1.ConditionReady,
		trafficv1alpha1.ConditionProgressing,
		trafficv1alpha1.ConditionDegraded,
		trafficv1alpha1.ConditionSuspended,
	} {
		status := metav1.ConditionFalse
		if conditionType == state.activeCondition {
			status = metav1.ConditionTrue
		}
		apimeta.SetStatusCondition(&scenario.Status.Conditions, metav1.Condition{
			Type:               conditionType,
			Status:             status,
			ObservedGeneration: scenario.Generation,
			Reason:             state.reason,
			Message:            state.message,
		})
	}

	if apiequality.Semantic.DeepEqual(before.Status, scenario.Status) {
		return nil
	}
	if err := r.Status().Patch(ctx, scenario, client.MergeFrom(before)); err != nil {
		return fmt.Errorf("update TrafficScenario status: %w", err)
	}
	return nil
}

func stateFromDeployment(deployment *appsv1.Deployment) scenarioState {
	desiredReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	}

	if deployment.Status.ObservedGeneration >= deployment.Generation {
		if condition := deploymentCondition(deployment, appsv1.DeploymentProgressing); condition != nil &&
			condition.Status == corev1.ConditionFalse &&
			condition.Reason == "ProgressDeadlineExceeded" {
			return degradedState(
				trafficv1alpha1.PhaseDegraded,
				reasonRolloutFailed,
				condition.Message,
			)
		}
		if condition := deploymentCondition(deployment, appsv1.DeploymentReplicaFailure); condition != nil &&
			condition.Status == corev1.ConditionTrue {
			return degradedState(
				trafficv1alpha1.PhaseDegraded,
				reasonRolloutFailed,
				condition.Message,
			)
		}
		if deployment.Status.UpdatedReplicas >= desiredReplicas &&
			deployment.Status.AvailableReplicas >= desiredReplicas &&
			deployment.Status.UnavailableReplicas == 0 {
			return scenarioState{
				phase:           trafficv1alpha1.PhaseReady,
				activeCondition: trafficv1alpha1.ConditionReady,
				reason:          reasonRunnerReady,
				message:         "Runner Deployment is ready",
			}
		}
	}

	return progressingState(
		reasonRunnerProgressing,
		fmt.Sprintf(
			"Runner Deployment rollout is progressing: %d/%d replicas available",
			deployment.Status.AvailableReplicas,
			desiredReplicas,
		),
	)
}

func deploymentCondition(
	deployment *appsv1.Deployment,
	conditionType appsv1.DeploymentConditionType,
) *appsv1.DeploymentCondition {
	for index := range deployment.Status.Conditions {
		condition := &deployment.Status.Conditions[index]
		if condition.Type == conditionType {
			return condition
		}
	}
	return nil
}
