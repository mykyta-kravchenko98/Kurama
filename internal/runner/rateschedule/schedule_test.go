package rateschedule

import (
	"context"
	"errors"
	"testing"
)

func TestFixedReturnsSameRateAtEveryInstant(t *testing.T) {
	t.Parallel()
	schedule := NewFixed(45)
	for range 3 {
		got, err := schedule.RequestsPerMinute(context.Background())
		if err != nil || got != 45 {
			t.Fatalf("RequestsPerMinute() = (%d, %v), want (45, nil)", got, err)
		}
	}
}

func TestFixedHonorsContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewFixed(45).RequestsPerMinute(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RequestsPerMinute() error = %v, want context.Canceled", err)
	}
}
