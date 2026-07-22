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
	NextDelay() time.Duration
}

type randomSource interface {
	Int64N(n int64) int64
}

func New(profileType string, rpm int) (Timing, error) {
	return newWithRandomSource(profileType, rpm, globalRandomSource{})
}

func newWithRandomSource(profileType string, rpm int, random randomSource) (Timing, error) {
	if rpm < 1 {
		return nil, fmt.Errorf("requests per minute must be positive")
	}
	meanDelay := time.Minute / time.Duration(rpm)
	if meanDelay < time.Nanosecond {
		return nil, fmt.Errorf("requests per minute is too large to schedule")
	}
	switch profileType {
	case "", TypeFixed:
		return fixedProfile{delay: meanDelay}, nil
	case TypeUniform:
		if random == nil {
			return nil, fmt.Errorf("random source must not be nil")
		}
		return uniformProfile{maxDelay: 2 * meanDelay, random: random}, nil
	default:
		return nil, fmt.Errorf("profile type %q is unsupported; use fixed or uniform", profileType)
	}
}

type fixedProfile struct {
	delay time.Duration
}

func (p fixedProfile) NextDelay() time.Duration {
	return p.delay
}

type uniformProfile struct {
	maxDelay time.Duration
	random   randomSource
}

func (p uniformProfile) NextDelay() time.Duration {
	return time.Duration(p.random.Int64N(int64(p.maxDelay))) + 1
}

type globalRandomSource struct{}

func (globalRandomSource) Int64N(n int64) int64 {
	return rand.Int64N(n)
}
