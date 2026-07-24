package profile

import (
	"fmt"
	"time"
)

const (
	TypeFixed                = "fixed"
	TypeUniform              = "uniform"
	TypeBurst                = "burst"
	DefaultBurstDelayDivisor = 10
	MinimumBurstDelayDivisor = 2
	MaximumBurstDelayDivisor = 100
)

type Config struct {
	Type         string
	MinBurstSize int
	MaxBurstSize int
	DelayDivisor int
}

// DelayContext describes the wait after an executed request within a reserved
// request batch. It intentionally contains the superset needed by all
// profiles: fixed and uniform only use RequestsPerMinute, while burst also
// needs Position and BatchSize. This is a deliberate compromise that keeps
// one small Timing contract for the scheduler without growing positional
// method arguments or splitting profiles into scheduler-specific interfaces.
type DelayContext struct {
	RequestsPerMinute int
	Position          int
	BatchSize         int
}

type Timing interface {
	BatchSize(requestsPerMinute int) int
	NextDelay(input DelayContext) time.Duration
}

func New(config Config) (Timing, error) {
	return newWithRandomSource(config, globalRandomSource{})
}

func newWithRandomSource(config Config, random randomSource) (Timing, error) {
	switch config.Type {
	case "", TypeFixed:
		if config.MinBurstSize != 0 || config.MaxBurstSize != 0 || config.DelayDivisor != 0 {
			return nil, fmt.Errorf("fixed profile must not set burst fields")
		}
		return fixedProfile{}, nil
	case TypeUniform:
		if config.MinBurstSize != 0 || config.MaxBurstSize != 0 || config.DelayDivisor != 0 {
			return nil, fmt.Errorf("uniform profile must not set burst fields")
		}
		if random == nil {
			return nil, fmt.Errorf("random source must not be nil")
		}
		return uniformProfile{random: random}, nil
	case TypeBurst:
		if config.MinBurstSize < 2 {
			return nil, fmt.Errorf("burst profile min burst size must be at least 2")
		}
		if config.MaxBurstSize < config.MinBurstSize {
			return nil, fmt.Errorf("burst profile max burst size must be at least min burst size")
		}
		delayDivisor := config.DelayDivisor
		if delayDivisor == 0 {
			delayDivisor = DefaultBurstDelayDivisor
		}
		if delayDivisor < MinimumBurstDelayDivisor || delayDivisor > MaximumBurstDelayDivisor {
			return nil, fmt.Errorf(
				"burst profile delay divisor must be between %d and %d",
				MinimumBurstDelayDivisor,
				MaximumBurstDelayDivisor,
			)
		}
		if random == nil {
			return nil, fmt.Errorf("random source must not be nil")
		}
		return &burstProfile{
			random:       random,
			minBurstSize: config.MinBurstSize,
			maxBurstSize: config.MaxBurstSize,
			delayDivisor: delayDivisor,
		}, nil
	default:
		return nil, fmt.Errorf("profile type %q is unsupported; use fixed, uniform or burst", config.Type)
	}
}

func meanDelay(requestsPerMinute int) time.Duration {
	return time.Minute / time.Duration(requestsPerMinute)
}
