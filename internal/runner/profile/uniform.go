package profile

import "time"

type uniformProfile struct {
	random randomSource
}

func (uniformProfile) BatchSize(int) int {
	return 1
}

func (p uniformProfile) NextDelay(input DelayContext) time.Duration {
	maxDelay := 2 * meanDelay(input.RequestsPerMinute)
	return time.Duration(p.random.Int64N(int64(maxDelay))) + 1
}
