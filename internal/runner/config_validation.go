package runner

import (
	"fmt"
	"net/url"
)

// Validate rejects ambiguous, unsafe or currently unsupported scenarios
// before a runner starts sending traffic.
func (c Config) Validate() error {
	if err := validateTarget(c.Target); err != nil {
		return err
	}
	if err := validateRateSchedule(c.Rate.Schedule); err != nil {
		return err
	}
	if c.Rate.Limiter != nil {
		switch c.Rate.Limiter.Type {
		case "", "local", "redis":
		default:
			return fmt.Errorf("rate.limiter.type %q is unsupported; use local or redis", c.Rate.Limiter.Type)
		}
	}
	if err := validateRateProfile(c.Rate.Profile); err != nil {
		return err
	}
	if len(c.Stores) > MaxStores {
		return fmt.Errorf("stores must contain at most %d entries", MaxStores)
	}

	stores := make(map[string]struct{}, len(c.Stores))
	for i, store := range c.Stores {
		if err := validateName(store.Name); err != nil {
			return fmt.Errorf("stores[%d].name: %w", i, err)
		}
		if _, exists := stores[store.Name]; exists {
			return fmt.Errorf("stores[%d].name %q is duplicated", i, store.Name)
		}
		if store.Capacity < 1 || store.Capacity > MaxStoreCapacity {
			return fmt.Errorf("stores[%d].capacity must be between 1 and %d", i, MaxStoreCapacity)
		}
		stores[store.Name] = struct{}{}
	}

	if len(c.Operations) == 0 || len(c.Operations) > MaxOperations {
		return fmt.Errorf("operations must contain between 1 and %d entries", MaxOperations)
	}
	operationNames := make(map[string]struct{}, len(c.Operations))
	for i, operation := range c.Operations {
		if err := validateOperation(operation, stores); err != nil {
			return fmt.Errorf("operations[%d]: %w", i, err)
		}
		if _, exists := operationNames[operation.Name]; exists {
			return fmt.Errorf("operations[%d].name %q is duplicated", i, operation.Name)
		}
		operationNames[operation.Name] = struct{}{}
	}
	return nil
}

func validateRateProfile(profile *RateProfileConfig) error {
	if profile == nil {
		return nil
	}
	switch profile.Type {
	case "", "fixed", "uniform":
		if profile.MinBurstSize != 0 || profile.MaxBurstSize != 0 || profile.DelayDivisor != 0 {
			return fmt.Errorf("rate.profile %s must not set burst fields", normalizedProfileType(profile.Type))
		}
	case "burst":
		if profile.MinBurstSize < 2 || profile.MinBurstSize > MaxProfileBurstSize {
			return fmt.Errorf("rate.profile.minBurstSize must be between 2 and %d", MaxProfileBurstSize)
		}
		if profile.MaxBurstSize < profile.MinBurstSize || profile.MaxBurstSize > MaxProfileBurstSize {
			return fmt.Errorf(
				"rate.profile.maxBurstSize must be between minBurstSize and %d",
				MaxProfileBurstSize,
			)
		}
		if profile.DelayDivisor != 0 &&
			(profile.DelayDivisor < 2 || profile.DelayDivisor > MaxProfileDelayDivisor) {
			return fmt.Errorf(
				"rate.profile.delayDivisor must be between 2 and %d",
				MaxProfileDelayDivisor,
			)
		}
	default:
		return fmt.Errorf("rate.profile.type %q is unsupported; use fixed, uniform or burst", profile.Type)
	}
	return nil
}

func normalizedProfileType(profileType string) string {
	if profileType == "" {
		return "fixed"
	}
	return profileType
}

func validateRateSchedule(schedule RateScheduleConfig) error {
	switch schedule.Type {
	case "fixed":
		if schedule.RequestsPerMinute < 1 || schedule.RequestsPerMinute > MaxRequestsPerMinute {
			return fmt.Errorf("rate.schedule.requestsPerMinute must be between 1 and %d for fixed schedule", MaxRequestsPerMinute)
		}
		if schedule.MinRequestsPerMinute != 0 || schedule.MaxRequestsPerMinute != 0 || schedule.WindowMinutes != 0 {
			return fmt.Errorf("rate.schedule fixed must not set uniform schedule fields")
		}
	case "uniform":
		if schedule.RequestsPerMinute != 0 {
			return fmt.Errorf("rate.schedule uniform must not set requestsPerMinute")
		}
		if schedule.MinRequestsPerMinute < 1 || schedule.MinRequestsPerMinute > MaxRequestsPerMinute {
			return fmt.Errorf("rate.schedule.minRequestsPerMinute must be between 1 and %d", MaxRequestsPerMinute)
		}
		if schedule.MaxRequestsPerMinute < schedule.MinRequestsPerMinute ||
			schedule.MaxRequestsPerMinute > MaxRequestsPerMinute {
			return fmt.Errorf(
				"rate.schedule.maxRequestsPerMinute must be between minRequestsPerMinute and %d",
				MaxRequestsPerMinute,
			)
		}
		if schedule.WindowMinutes < 1 || schedule.WindowMinutes > MaxScheduleWindowMinutes {
			return fmt.Errorf("rate.schedule.windowMinutes must be between 1 and %d", MaxScheduleWindowMinutes)
		}
	default:
		return fmt.Errorf("rate.schedule.type %q is unsupported; use fixed or uniform", schedule.Type)
	}
	return nil
}

func validateTarget(target TargetConfig) error {
	parsed, err := url.ParseRequestURI(target.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("target.baseURL must be an absolute HTTP(S) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("target.baseURL scheme %q is unsupported", parsed.Scheme)
	}
	if parsed.User != nil {
		return fmt.Errorf("target.baseURL must not contain credentials")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("target.baseURL must not contain a query or fragment")
	}
	return nil
}
