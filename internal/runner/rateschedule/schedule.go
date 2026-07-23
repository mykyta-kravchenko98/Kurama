package rateschedule

import "time"

type Schedule interface {
	RequestsPerMinute(now time.Time) int
}

type Fixed struct {
	requestsPerMinute int
}

func NewFixed(requestsPerMinute int) Fixed {
	return Fixed{requestsPerMinute: requestsPerMinute}
}

func (s Fixed) RequestsPerMinute(time.Time) int {
	return s.requestsPerMinute
}
