package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
)

func TestDeleteScenarioRedisKeysOnlyDeletesMatchingUID(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() {
		if err := redisClient.Close(); err != nil {
			t.Errorf("close Redis client: %v", err)
		}
	}()

	matchingKeys := []string{
		"kurama:v1:shorturl:load:old-uid:hashes",
		"kurama:v1:rate:shorturl:load:old-uid",
		"kurama:v1:rate-schedule:shorturl:load:old-uid:2:128:60000000",
	}
	preservedKeys := []string{
		"kurama:v1:shorturl:load:new-uid:hashes",
		"kurama:v1:rate:shorturl:load:new-uid",
		"kurama:v1:rate-schedule:shorturl:load:new-uid:2:128:60000000",
		"kurama:v1:other:load:old-uid:hashes",
	}
	for _, key := range append(matchingKeys, preservedKeys...) {
		setRedisKey(t, server, key)
	}

	if err := deleteScenarioRedisKeys(context.Background(), redisClient, redisCleanupScope{
		Namespace: "shorturl",
		Scenario:  "load",
		UID:       "old-uid",
	}); err != nil {
		t.Fatalf("deleteScenarioRedisKeys() error = %v", err)
	}
	for _, key := range matchingKeys {
		if server.Exists(key) {
			t.Errorf("matching key %q still exists", key)
		}
	}
	for _, key := range preservedKeys {
		if !server.Exists(key) {
			t.Errorf("unrelated key %q was deleted", key)
		}
	}
}

func TestReconcileDeletionCleansRedisAndRemovesFinalizer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := newScheme(t)
	now := metav1.NewTime(time.Now())
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "shorturl",
			Namespace:         "shorturl",
			UID:               types.UID("scenario-uid"),
			DeletionTimestamp: &now,
			Finalizers:        []string{redisCleanupFinalizer},
		},
		Spec: validScenarioSpec(),
	}
	server := miniredis.RunT(t)
	setRedisKey(t, server, "kurama:v1:shorturl:shorturl:scenario-uid:hashes")
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() {
		if err := redisClient.Close(); err != nil {
			t.Errorf("close Redis client: %v", err)
		}
	}()
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(scenario).
		WithObjects(scenario).
		Build()
	reconciler := &TrafficScenarioReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		RedisClient: redisClient,
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(scenario)}); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if server.Exists("kurama:v1:shorturl:shorturl:scenario-uid:hashes") {
		t.Fatal("scenario Redis key still exists")
	}

	var actual trafficv1alpha1.TrafficScenario
	err := fakeClient.Get(ctx, client.ObjectKeyFromObject(scenario), &actual)
	if err == nil && controllerutil.ContainsFinalizer(&actual, redisCleanupFinalizer) {
		t.Fatal("Redis cleanup finalizer still exists")
	}
	if err != nil && client.IgnoreNotFound(err) != nil {
		t.Fatalf("get TrafficScenario after finalization: %v", err)
	}
}

func TestReconcileDeletionStopsRunnerBeforeRedisCleanup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := newScheme(t)
	now := metav1.NewTime(time.Now())
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "shorturl",
			Namespace:         "shorturl",
			UID:               types.UID("scenario-uid"),
			DeletionTimestamp: &now,
			Finalizers:        []string{redisCleanupFinalizer},
		},
		Spec: validScenarioSpec(),
	}
	deployment := desiredDeployment(scenario, runnerName(scenario.Name), "example.test/kurama:test", "", "")
	if err := controllerutil.SetControllerReference(scenario, deployment, scheme); err != nil {
		t.Fatalf("set runner Deployment owner: %v", err)
	}
	server := miniredis.RunT(t)
	key := "kurama:v1:shorturl:shorturl:scenario-uid:hashes"
	setRedisKey(t, server, key)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() {
		if err := redisClient.Close(); err != nil {
			t.Errorf("close Redis client: %v", err)
		}
	}()
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(scenario).
		WithObjects(scenario, deployment).
		Build()
	reconciler := &TrafficScenarioReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		RedisClient: redisClient,
	}

	result, err := reconciler.Reconcile(ctx, requestFor(scenario))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.RequeueAfter != deletionRequeueInterval {
		t.Fatalf("RequeueAfter = %v, want %v", result.RequeueAfter, deletionRequeueInterval)
	}
	if !server.Exists(key) {
		t.Fatal("Redis data was deleted before the runner Deployment stopped")
	}
}

func TestReconcileDeletionRetriesCleanupBeforeGracePeriodExpires(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := newScheme(t)
	now := metav1.NewTime(time.Now())
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "shorturl",
			Namespace:         "shorturl",
			UID:               types.UID("scenario-uid"),
			DeletionTimestamp: &now,
			Finalizers:        []string{redisCleanupFinalizer},
		},
		Spec: validScenarioSpec(),
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(scenario).
		WithObjects(scenario).
		Build()
	reconciler := &TrafficScenarioReconciler{Client: fakeClient, Scheme: scheme}

	result, err := reconciler.Reconcile(ctx, requestFor(scenario))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.RequeueAfter != deletionRequeueInterval {
		t.Fatalf("RequeueAfter = %v, want %v", result.RequeueAfter, deletionRequeueInterval)
	}
	var actual trafficv1alpha1.TrafficScenario
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(scenario), &actual); err != nil {
		t.Fatalf("get TrafficScenario: %v", err)
	}
	if !controllerutil.ContainsFinalizer(&actual, redisCleanupFinalizer) {
		t.Fatal("Redis cleanup finalizer was removed before the grace period expired")
	}
}

func TestReconcileDeletionAbandonsCleanupAfterGracePeriod(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := newScheme(t)
	expired := metav1.NewTime(time.Now().Add(-redisCleanupGracePeriod - time.Second))
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "shorturl",
			Namespace:         "shorturl",
			UID:               types.UID("scenario-uid"),
			DeletionTimestamp: &expired,
			Finalizers:        []string{redisCleanupFinalizer},
		},
		Spec: validScenarioSpec(),
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(scenario).
		WithObjects(scenario).
		Build()
	observer := &recordingRedisCleanupObserver{}
	recorder := record.NewFakeRecorder(1)
	reconciler := &TrafficScenarioReconciler{
		Client:               fakeClient,
		Scheme:               scheme,
		RedisCleanupObserver: observer,
		EventRecorder:        recorder,
	}

	result, err := reconciler.Reconcile(ctx, requestFor(scenario))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("RequeueAfter = %v, want zero", result.RequeueAfter)
	}
	if observer.abandoned != 1 {
		t.Fatalf("abandoned observations = %d, want 1", observer.abandoned)
	}
	select {
	case event := <-recorder.Events:
		if !strings.Contains(event, "Warning "+reasonCleanupAbandoned) {
			t.Fatalf("event = %q, want Warning %s", event, reasonCleanupAbandoned)
		}
		if !strings.Contains(event, redisCleanupGracePeriod.String()) {
			t.Fatalf("event = %q, want grace period %s", event, redisCleanupGracePeriod)
		}
	default:
		t.Fatal("cleanup abandonment Warning Event was not recorded")
	}

	var actual trafficv1alpha1.TrafficScenario
	err = fakeClient.Get(ctx, client.ObjectKeyFromObject(scenario), &actual)
	if err == nil && controllerutil.ContainsFinalizer(&actual, redisCleanupFinalizer) {
		t.Fatal("Redis cleanup finalizer still exists after the grace period")
	}
	if err != nil && client.IgnoreNotFound(err) != nil {
		t.Fatalf("get TrafficScenario after forced finalization: %v", err)
	}
}

func TestSuspendedScenarioKeepsRedisCleanupFinalizer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := newScheme(t)
	server := miniredis.RunT(t)
	key := "kurama:v1:shorturl:shorturl:scenario-uid:hashes"
	setRedisKey(t, server, key)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() {
		if err := redisClient.Close(); err != nil {
			t.Errorf("close Redis client: %v", err)
		}
	}()
	scenario := &trafficv1alpha1.TrafficScenario{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "shorturl",
			Namespace: "shorturl",
			UID:       types.UID("scenario-uid"),
		},
		Spec: trafficv1alpha1.TrafficScenarioSpec{
			Suspend: true,
			Storage: &trafficv1alpha1.StorageSpec{Type: trafficv1alpha1.StorageTypeRedis},
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(scenario).
		WithObjects(scenario).
		Build()
	reconciler := &TrafficScenarioReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		RedisClient: redisClient,
	}

	if _, err := reconciler.Reconcile(ctx, requestFor(scenario)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	var actual trafficv1alpha1.TrafficScenario
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(scenario), &actual); err != nil {
		t.Fatalf("get suspended TrafficScenario: %v", err)
	}
	if !controllerutil.ContainsFinalizer(&actual, redisCleanupFinalizer) {
		t.Fatal("suspended Redis scenario did not receive cleanup finalizer")
	}
	if !server.Exists(key) {
		t.Fatal("suspend deleted scenario Redis data")
	}
}

func setRedisKey(t *testing.T, server *miniredis.Miniredis, key string) {
	t.Helper()
	if err := server.Set(key, "value"); err != nil {
		t.Fatalf("set Redis key %q: %v", key, err)
	}
}

type recordingRedisCleanupObserver struct {
	abandoned int
}

func (o *recordingRedisCleanupObserver) ObserveAbandoned() {
	o.abandoned++
}
