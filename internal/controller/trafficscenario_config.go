package controller

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
)

func runnerReplicas(scenario *trafficv1alpha1.TrafficScenario) int32 {
	if scenario.Spec.Replicas == 0 {
		return 1
	}
	return scenario.Spec.Replicas
}

func rateLimiterBackend(scenario *trafficv1alpha1.TrafficScenario) string {
	if scenario.Spec.Rate.Limiter != nil && scenario.Spec.Rate.Limiter.Type != "" {
		return string(scenario.Spec.Rate.Limiter.Type)
	}
	if storageBackend(scenario) == string(trafficv1alpha1.StorageTypeRedis) {
		return string(trafficv1alpha1.RateLimiterTypeRedis)
	}
	return string(trafficv1alpha1.RateLimiterTypeLocal)
}

func rateProfileType(scenario *trafficv1alpha1.TrafficScenario) string {
	if scenario.Spec.Rate.Profile == nil || scenario.Spec.Rate.Profile.Type == "" {
		return string(trafficv1alpha1.RateProfileTypeFixed)
	}
	return string(scenario.Spec.Rate.Profile.Type)
}

func requiresRedis(scenario *trafficv1alpha1.TrafficScenario) bool {
	return storageBackend(scenario) == string(trafficv1alpha1.StorageTypeRedis) ||
		rateLimiterBackend(scenario) == string(trafficv1alpha1.RateLimiterTypeRedis) ||
		scenario.Spec.Rate.Schedule.Type == trafficv1alpha1.RateScheduleTypeUniform
}

func storageBackend(scenario *trafficv1alpha1.TrafficScenario) string {
	if scenario.Spec.Storage == nil || scenario.Spec.Storage.Type == "" {
		return string(trafficv1alpha1.StorageTypeMemory)
	}
	return string(scenario.Spec.Storage.Type)
}

func scenarioRunnerConfig(scenario *trafficv1alpha1.TrafficScenario) runner.Config {
	config := runner.Config{
		Target: runner.TargetConfig{BaseURL: scenario.Spec.Target.BaseURL},
		Rate: runner.RateConfig{
			Schedule: runner.RateScheduleConfig{
				Type:                 string(scenario.Spec.Rate.Schedule.Type),
				RequestsPerMinute:    scenario.Spec.Rate.Schedule.RequestsPerMinute,
				MinRequestsPerMinute: scenario.Spec.Rate.Schedule.MinRequestsPerMinute,
				MaxRequestsPerMinute: scenario.Spec.Rate.Schedule.MaxRequestsPerMinute,
				WindowMinutes:        scenario.Spec.Rate.Schedule.WindowMinutes,
			},
			Limiter: &runner.RateLimiterConfig{
				Type: rateLimiterBackend(scenario),
			},
			Profile: &runner.RateProfileConfig{
				Type:         rateProfileType(scenario),
				MinBurstSize: rateProfileMinBurstSize(scenario),
				MaxBurstSize: rateProfileMaxBurstSize(scenario),
				DelayDivisor: rateProfileDelayDivisor(scenario),
			},
		},
		Stores:     make([]runner.StoreConfig, len(scenario.Spec.Stores)),
		Operations: make([]runner.OperationConfig, len(scenario.Spec.Operations)),
	}
	for i, store := range scenario.Spec.Stores {
		config.Stores[i] = runner.StoreConfig{Name: store.Name, Capacity: store.Capacity}
	}
	for i, operation := range scenario.Spec.Operations {
		converted := runner.OperationConfig{
			Name:   operation.Name,
			Weight: operation.Weight,
			Request: runner.RequestConfig{
				Method:       operation.Request.Method,
				PathTemplate: operation.Request.PathTemplate,
				Headers:      operation.Request.Headers,
				BodyTemplate: operation.Request.BodyTemplate,
				Variables:    make([]runner.VariableConfig, len(operation.Request.Variables)),
			},
			ExpectedStatusCodes: operation.ExpectedStatusCodes,
		}
		for j, variable := range operation.Request.Variables {
			converted.Request.Variables[j] = runner.VariableConfig{
				Name: variable.Name,
				Source: runner.VariableSource{
					Type:   variable.Source.Type,
					Store:  variable.Source.Store,
					Length: variable.Source.Length,
				},
			}
		}
		if operation.Capture != nil {
			converted.Capture = &runner.CaptureConfig{
				JSONPointer: operation.Capture.JSONPointer,
				Store:       operation.Capture.Store,
			}
		}
		config.Operations[i] = converted
	}
	return config
}

func rateProfileMinBurstSize(scenario *trafficv1alpha1.TrafficScenario) int {
	if scenario.Spec.Rate.Profile == nil {
		return 0
	}
	return scenario.Spec.Rate.Profile.MinBurstSize
}

func rateProfileMaxBurstSize(scenario *trafficv1alpha1.TrafficScenario) int {
	if scenario.Spec.Rate.Profile == nil {
		return 0
	}
	return scenario.Spec.Rate.Profile.MaxBurstSize
}

func rateProfileDelayDivisor(scenario *trafficv1alpha1.TrafficScenario) int {
	if scenario.Spec.Rate.Profile == nil {
		return 0
	}
	return scenario.Spec.Rate.Profile.DelayDivisor
}

func scenarioConfigJSON(scenario *trafficv1alpha1.TrafficScenario) string {
	data, _ := json.Marshal(scenarioRunnerConfig(scenario))
	return string(data)
}

func configHash(config string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(config)))
}
