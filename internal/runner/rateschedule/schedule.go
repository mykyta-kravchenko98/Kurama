package rateschedule

import "context"

type Schedule interface {
	RequestsPerMinute(ctx context.Context) (int, error)
}

// Fixed always supplies the configured request budget.
type Fixed struct {
	requestsPerMinute int
}

// NewFixed creates a schedule whose RPM never changes.
func NewFixed(requestsPerMinute int) Fixed {
	return Fixed{requestsPerMinute: requestsPerMinute}
}

func (s Fixed) RequestsPerMinute(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.requestsPerMinute, nil
}
