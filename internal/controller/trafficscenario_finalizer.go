package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/redis/go-redis/v9"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner/rediskey"
)

const (
	redisCleanupFinalizer   = "traffic.kurama.dev/redis-cleanup"
	redisCleanupScanCount   = 100
	redisCleanupGracePeriod = 5 * time.Minute
	deletionRequeueInterval = rolloutRequeueInterval
	reasonCleanupAbandoned  = "RedisCleanupAbandoned"
)

func (r *TrafficScenarioReconciler) reconcileDeletion(
	ctx context.Context,
	scenario *trafficv1alpha1.TrafficScenario,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(scenario, redisCleanupFinalizer) {
		return ctrl.Result{}, nil
	}

	deployment := &appsv1.Deployment{}
	key := types.NamespacedName{Namespace: scenario.Namespace, Name: runnerName(scenario.Name)}
	if err := r.Get(ctx, key, deployment); err != nil && !apierrors.IsNotFound(err) {
		return r.retryOrAbandonCleanup(
			ctx,
			scenario,
			fmt.Errorf("get runner Deployment before Redis cleanup: %w", err),
		)
	} else if err == nil {
		if err := ensureControlledBy(deployment, scenario, "Deployment"); err != nil {
			return r.retryOrAbandonCleanup(ctx, scenario, err)
		}
		propagation := metav1.DeletePropagationForeground
		if err := r.Delete(ctx, deployment, &client.DeleteOptions{PropagationPolicy: &propagation}); err != nil &&
			!apierrors.IsNotFound(err) {
			return r.retryOrAbandonCleanup(
				ctx,
				scenario,
				fmt.Errorf("delete runner Deployment before Redis cleanup: %w", err),
			)
		}
		if redisCleanupGracePeriodExpired(scenario) {
			return r.abandonCleanup(
				ctx,
				scenario,
				fmt.Errorf("grace period expired while waiting for runner Deployment deletion"),
			)
		}
		return ctrl.Result{RequeueAfter: deletionRequeueInterval}, nil
	}

	if r.RedisClient == nil {
		return r.retryOrAbandonCleanup(
			ctx,
			scenario,
			fmt.Errorf("redis client is unavailable for TrafficScenario cleanup"),
		)
	}
	keyScope, err := rediskey.NewScope(scenario.Namespace, scenario.Name, string(scenario.UID))
	if err != nil {
		return r.retryOrAbandonCleanup(ctx, scenario, err)
	}
	if err := deleteScenarioRedisKeys(ctx, r.RedisClient, keyScope); err != nil {
		return r.retryOrAbandonCleanup(ctx, scenario, err)
	}

	return r.removeRedisCleanupFinalizer(ctx, scenario)
}

func (r *TrafficScenarioReconciler) retryOrAbandonCleanup(
	ctx context.Context,
	scenario *trafficv1alpha1.TrafficScenario,
	cause error,
) (ctrl.Result, error) {
	if redisCleanupGracePeriodExpired(scenario) {
		return r.abandonCleanup(ctx, scenario, cause)
	}
	ctrl.LoggerFrom(ctx).Error(
		cause,
		"TrafficScenario cleanup failed; retrying before grace period expires",
		"requeueAfter",
		deletionRequeueInterval,
	)
	return ctrl.Result{RequeueAfter: deletionRequeueInterval}, nil
}

func (r *TrafficScenarioReconciler) abandonCleanup(
	ctx context.Context,
	scenario *trafficv1alpha1.TrafficScenario,
	cause error,
) (ctrl.Result, error) {
	message := fmt.Sprintf(
		"Redis cleanup did not complete within %s; removing the finalizer and leaving UID-scoped keys for later cleanup: %v",
		redisCleanupGracePeriod,
		cause,
	)
	ctrl.LoggerFrom(ctx).Error(
		cause,
		"TrafficScenario cleanup grace period expired; removing finalizer",
		"gracePeriod",
		redisCleanupGracePeriod,
	)
	result, err := r.removeRedisCleanupFinalizer(ctx, scenario)
	if err != nil {
		return result, err
	}
	if r.EventRecorder != nil {
		r.EventRecorder.Event(scenario, corev1.EventTypeWarning, reasonCleanupAbandoned, message)
	}
	if r.RedisCleanupObserver != nil {
		r.RedisCleanupObserver.ObserveAbandoned()
	}
	return result, nil
}

func (r *TrafficScenarioReconciler) removeRedisCleanupFinalizer(
	ctx context.Context,
	scenario *trafficv1alpha1.TrafficScenario,
) (ctrl.Result, error) {
	controllerutil.RemoveFinalizer(scenario, redisCleanupFinalizer)
	if err := r.Update(ctx, scenario); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove Redis cleanup finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

func redisCleanupGracePeriodExpired(scenario *trafficv1alpha1.TrafficScenario) bool {
	if scenario.DeletionTimestamp.IsZero() {
		return false
	}
	return !time.Now().Before(scenario.DeletionTimestamp.Add(redisCleanupGracePeriod))
}

func deleteScenarioRedisKeys(
	ctx context.Context,
	redisClient redis.UniversalClient,
	scope rediskey.Scope,
) error {
	if redisClient == nil {
		return fmt.Errorf("redis client must not be nil")
	}

	keys := make([]string, 0)
	for _, pattern := range scope.CleanupPatterns() {
		iterator := redisClient.Scan(ctx, 0, pattern, redisCleanupScanCount).Iterator()
		for iterator.Next(ctx) {
			keys = append(keys, iterator.Val())
		}
		if err := iterator.Err(); err != nil {
			return fmt.Errorf("scan Redis keys for TrafficScenario cleanup: %w", err)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	if err := redisClient.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("delete Redis keys for TrafficScenario cleanup: %w", err)
	}
	return nil
}
