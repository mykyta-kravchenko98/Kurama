package profile

import (
	"testing"
	"time"
)

func TestUniformProfileUsesFullRangeAroundMean(t *testing.T) {
	t.Parallel()
	random := &sequenceRandomSource{values: []int64{0, int64(4*time.Second) - 1}}
	profile, err := newWithRandomSource(Config{Type: TypeUniform}, random)
	if err != nil {
		t.Fatalf("newWithRandomSource() error = %v", err)
	}
	if got := profile.BatchSize(30); got != 1 {
		t.Fatalf("BatchSize() = %d, want 1", got)
	}
	context := DelayContext{RequestsPerMinute: 30, Position: 1, BatchSize: 1}
	if got := profile.NextDelay(context); got != time.Nanosecond {
		t.Fatalf("minimum NextDelay() = %s, want 1ns", got)
	}
	if got := profile.NextDelay(context); got != 4*time.Second {
		t.Fatalf("maximum NextDelay() = %s, want 4s", got)
	}
}
