// Package rediskey defines the versioned Redis key namespace shared by the
// runner and controller cleanup logic.
package rediskey

import (
	"fmt"
	"strings"
)

const (
	rootPrefix         = "kurama:v2"
	storeKind          = "store"
	rateLimitKind      = "rate-limit"
	rateScheduleKind   = "rate-schedule"
	separator          = ":"
	cleanupPatternTail = "*"
)

// Scope identifies Redis state owned by one immutable TrafficScenario.
type Scope struct {
	namespace string
	scenario  string
	uid       string
}

// NewScope validates the key components and returns an immutable scope.
func NewScope(namespace, scenario, uid string) (Scope, error) {
	scope := Scope{namespace: namespace, scenario: scenario, uid: uid}
	if err := scope.Validate(); err != nil {
		return Scope{}, err
	}
	return scope, nil
}

// Validate rejects the zero value and any scope that cannot be encoded
// unambiguously into Kurama's colon-delimited Redis key format.
func (s Scope) Validate() error {
	for name, value := range map[string]string{
		"namespace": s.namespace,
		"scenario":  s.scenario,
		"UID":       s.uid,
	} {
		if value == "" {
			return fmt.Errorf("redis scope %s must not be empty", name)
		}
		if strings.Contains(value, separator) {
			return fmt.Errorf("redis scope %s must not contain colon", name)
		}
	}
	return nil
}

// StorePrefix returns the prefix shared by all named value stores.
func (s Scope) StorePrefix() string {
	return s.kindPrefix(storeKind)
}

// RateLimitKey returns the single shared-budget key for the scenario.
func (s Scope) RateLimitKey() string {
	return s.kindPrefix(rateLimitKind)
}

// RateSchedulePrefix returns the prefix for parameterized schedule keys.
func (s Scope) RateSchedulePrefix() string {
	return s.kindPrefix(rateScheduleKind)
}

// CleanupPatterns returns every key pattern owned by the scenario.
func (s Scope) CleanupPatterns() []string {
	return []string{
		s.StorePrefix() + separator + cleanupPatternTail,
		s.RateLimitKey(),
		s.RateSchedulePrefix() + separator + cleanupPatternTail,
	}
}

func (s Scope) kindPrefix(kind string) string {
	return strings.Join([]string{
		rootPrefix,
		kind,
		s.namespace,
		s.scenario,
		s.uid,
	}, separator)
}
