package controller

import (
	"crypto/sha256"
	"fmt"
	"strings"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
)

const (
	maxRunnerNameLength       = 63
	runnerNameSuffix          = "-runner"
	scenarioHashLength        = 8
	scenarioNameAnnotation    = "traffic.kurama.dev/scenario-name"
	maxLabelValueLength       = 63
	hashedScenarioLabelPrefix = "sha256-"
)

func runnerName(scenarioName string) string {
	if len(scenarioName)+len(runnerNameSuffix) <= maxRunnerNameLength {
		return scenarioName + runnerNameSuffix
	}

	hash := scenarioHash(scenarioName)
	maxPrefixLength := maxRunnerNameLength - len(runnerNameSuffix) - 1 - len(hash)
	prefix := strings.TrimRight(scenarioName[:maxPrefixLength], ".-")
	return prefix + "-" + hash + runnerNameSuffix
}

func labels(scenario *trafficv1alpha1.TrafficScenario) map[string]string {
	return map[string]string{
		componentLabel: "runner",
		scenarioLabel:  scenarioLabelValue(scenario.Name),
	}
}

func scenarioLabelValue(scenarioName string) string {
	if len(scenarioName) <= maxLabelValueLength {
		return scenarioName
	}
	return hashedScenarioLabelPrefix + scenarioHash(scenarioName)
}

func scenarioAnnotations(scenario *trafficv1alpha1.TrafficScenario) map[string]string {
	if len(scenario.Name)+len(runnerNameSuffix) <= maxRunnerNameLength {
		return nil
	}
	return map[string]string{scenarioNameAnnotation: scenario.Name}
}

func scenarioHash(scenarioName string) string {
	sum := sha256.Sum256([]byte(scenarioName))
	return fmt.Sprintf("%x", sum)[:scenarioHashLength]
}
