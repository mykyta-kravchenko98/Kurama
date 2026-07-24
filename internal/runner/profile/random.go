package profile

import "math/rand/v2"

type randomSource interface {
	Int64N(n int64) int64
}

type globalRandomSource struct{}

func (globalRandomSource) Int64N(n int64) int64 {
	return rand.Int64N(n)
}
