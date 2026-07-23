package rateschedule

import (
	"testing"
	"time"
)

func TestFixedReturnsSameRateAtEveryInstant(t *testing.T) {
	t.Parallel()
	schedule := NewFixed(45)
	times := []time.Time{
		time.Unix(0, 0),
		time.Now(),
		time.Now().Add(24 * time.Hour),
	}
	for _, now := range times {
		if got := schedule.RequestsPerMinute(now); got != 45 {
			t.Fatalf("RequestsPerMinute(%s) = %d, want 45", now, got)
		}
	}
}
