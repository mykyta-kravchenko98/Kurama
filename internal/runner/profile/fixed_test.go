package profile

import (
	"testing"
	"time"
)

func TestFixedProfileUsesConfiguredMeanDelay(t *testing.T) {
	t.Parallel()
	profile, err := New(Config{Type: TypeFixed})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := profile.BatchSize(30); got != 1 {
		t.Fatalf("BatchSize() = %d, want 1", got)
	}
	if got := profile.NextDelay(DelayContext{RequestsPerMinute: 30, Position: 1, BatchSize: 1}); got != 2*time.Second {
		t.Fatalf("NextDelay() = %s, want 2s", got)
	}
}

func TestEmptyProfileTypeDefaultsToFixed(t *testing.T) {
	t.Parallel()
	profile, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := profile.NextDelay(DelayContext{RequestsPerMinute: 60, Position: 1, BatchSize: 1}); got != time.Second {
		t.Fatalf("NextDelay() = %s, want 1s", got)
	}
}
