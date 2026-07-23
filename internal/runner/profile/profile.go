package profile

import (
	"fmt"
	"math/rand/v2"
	"time"
)

const (
	TypeFixed   = "fixed"
	TypeUniform = "uniform"
)

type Timing interface {
	NextDelay(requestsPerMinute int) time.Duration
}

type randomSource interface {
	Int64N(n int64) int64
}

func New(profileType string) (Timing, error) {
	return newWithRandomSource(profileType, globalRandomSource{})
}

func newWithRandomSource(profileType string, random randomSource) (Timing, error) {
	switch profileType {
	case "", TypeFixed:
		return fixedProfile{}, nil
	case TypeUniform:
		if random == nil {
			return nil, fmt.Errorf("random source must not be nil")
		}
		return uniformProfile{random: random}, nil
	default:
		return nil, fmt.Errorf("profile type %q is unsupported; use fixed or uniform", profileType)
	}
}

type fixedProfile struct{}

func (fixedProfile) NextDelay(requestsPerMinute int) time.Duration {
	return meanDelay(requestsPerMinute)
}

type uniformProfile struct {
	random randomSource
}

func (p uniformProfile) NextDelay(requestsPerMinute int) time.Duration {
	maxDelay := 2 * meanDelay(requestsPerMinute)
	return time.Duration(p.random.Int64N(int64(maxDelay))) + 1
}

func meanDelay(requestsPerMinute int) time.Duration {
	return time.Minute / time.Duration(requestsPerMinute)
}

type globalRandomSource struct{}

func (globalRandomSource) Int64N(n int64) int64 {
	return rand.Int64N(n)
}
