package profile

import (
	"testing"
	"time"
)

func TestFixedProfileUsesConfiguredMeanDelay(t *testing.T) {
	t.Parallel()
	profile, err := New(TypeFixed)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := profile.NextDelay(30); got != 2*time.Second {
		t.Fatalf("NextDelay() = %s, want 2s", got)
	}
}

func TestEmptyProfileTypeDefaultsToFixed(t *testing.T) {
	t.Parallel()
	profile, err := New("")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := profile.NextDelay(60); got != time.Second {
		t.Fatalf("NextDelay() = %s, want 1s", got)
	}
}

func TestUniformProfileUsesFullRangeAroundMean(t *testing.T) {
	t.Parallel()
	random := &sequenceRandomSource{values: []int64{0, int64(4*time.Second) - 1}}
	profile, err := newWithRandomSource(TypeUniform, random)
	if err != nil {
		t.Fatalf("newWithRandomSource() error = %v", err)
	}
	if got := profile.NextDelay(30); got != time.Nanosecond {
		t.Fatalf("minimum NextDelay() = %s, want 1ns", got)
	}
	if got := profile.NextDelay(30); got != 4*time.Second {
		t.Fatalf("maximum NextDelay() = %s, want 4s", got)
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		profileType string
	}{
		{name: "unknown profile", profileType: "burst"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(test.profileType); err == nil {
				t.Fatal("New() error = nil")
			}
		})
	}
}

type sequenceRandomSource struct {
	values []int64
	next   int
}

func (s *sequenceRandomSource) Int64N(n int64) int64 {
	value := s.values[s.next]
	s.next++
	if value < 0 || value >= n {
		panic("test random value is outside Int64N range")
	}
	return value
}
