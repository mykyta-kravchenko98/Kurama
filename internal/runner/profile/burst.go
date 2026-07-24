package profile

import "time"

type burstProfile struct {
	random       randomSource
	minBurstSize int
	maxBurstSize int
	delayDivisor int
}

func (p *burstProfile) BatchSize(requestsPerMinute int) int {
	maxSize := min(p.maxBurstSize, requestsPerMinute)
	minSize := min(p.minBurstSize, maxSize)
	return minSize + int(p.random.Int64N(int64(maxSize-minSize+1)))
}

func (p *burstProfile) NextDelay(input DelayContext) time.Duration {
	mean := meanDelay(input.RequestsPerMinute)
	intraBurstDelay := mean / time.Duration(p.delayDivisor)
	if intraBurstDelay < time.Nanosecond {
		intraBurstDelay = time.Nanosecond
	}
	if input.Position < input.BatchSize {
		return intraBurstDelay
	}

	return time.Duration(input.BatchSize)*mean -
		time.Duration(input.BatchSize-1)*intraBurstDelay
}
