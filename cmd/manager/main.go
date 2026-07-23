package main

import (
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
	"github.com/mykyta-kravchenko98/Kurama/internal/controller"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
)

var scheme = runtime.NewScheme()

const healthProbeBindAddress = ":8081"

func init() {
	mustRegister(clientgoscheme.AddToScheme)
	mustRegister(trafficv1alpha1.AddToScheme)
}

func mustRegister(addToScheme func(*runtime.Scheme) error) {
	if err := addToScheme(scheme); err != nil {
		panic(err)
	}
}

func watchNamespaces() map[string]cache.Config {
	raw := os.Getenv("WATCH_NAMESPACES")
	if raw == "" {
		raw = os.Getenv("POD_NAMESPACE")
	}
	if raw == "" {
		return nil
	}

	namespaces := map[string]cache.Config{}
	for _, namespace := range strings.Split(raw, ",") {
		if namespace = strings.TrimSpace(namespace); namespace != "" {
			namespaces[namespace] = cache.Config{}
		}
	}
	return namespaces
}

func main() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(false)))
	logger := ctrl.Log.WithName("setup")

	runnerImage := os.Getenv("KURAMA_RUNNER_IMAGE")
	if runnerImage == "" {
		logger.Error(nil, "KURAMA_RUNNER_IMAGE must be set")
		os.Exit(1)
	}
	runnerImagePullSecret := os.Getenv("KURAMA_RUNNER_IMAGE_PULL_SECRET")
	redisAddress := os.Getenv(runner.RedisAddressEnv)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: healthProbeBindAddress,
		Cache: cache.Options{
			DefaultNamespaces: watchNamespaces(),
		},
	})
	if err != nil {
		logger.Error(err, "unable to start manager")
		os.Exit(1)
	}

	reconciler := &controller.TrafficScenarioReconciler{
		Client:                mgr.GetClient(),
		Scheme:                mgr.GetScheme(),
		RunnerImage:           runnerImage,
		RunnerImagePullSecret: runnerImagePullSecret,
		RedisAddress:          redisAddress,
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to set up TrafficScenario controller")
		os.Exit(1)
	}
	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		logger.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("ping", healthz.Ping); err != nil {
		logger.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	logger.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "manager exited with error")
		os.Exit(1)
	}
}
