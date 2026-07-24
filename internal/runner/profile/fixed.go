package profile

import "time"

type fixedProfile struct{}

func (fixedProfile) BatchSize(int) int {
	return 1
}

func (fixedProfile) NextDelay(input DelayContext) time.Duration {
	return meanDelay(input.RequestsPerMinute)
}
