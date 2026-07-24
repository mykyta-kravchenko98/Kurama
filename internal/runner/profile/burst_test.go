package profile

import (
	"testing"
	"time"
)

func TestBurstProfilePreservesMeanRateAcrossBurst(t *testing.T) {
	t.Parallel()
	random := &sequenceRandomSource{values: []int64{0}}
	profile, err := newWithRandomSource(Config{
		Type: TypeBurst, MinBurstSize: 3, MaxBurstSize: 3,
	}, random)
	if err != nil {
		t.Fatalf("newWithRandomSource() error = %v", err)
	}
	batchSize := profile.BatchSize(60)
	if batchSize != 3 {
		t.Fatalf("BatchSize() = %d, want 3", batchSize)
	}

	delays := []time.Duration{
		profile.NextDelay(delayContext(60, 1, batchSize)),
		profile.NextDelay(delayContext(60, 2, batchSize)),
		profile.NextDelay(delayContext(60, 3, batchSize)),
	}
	want := []time.Duration{100 * time.Millisecond, 100 * time.Millisecond, 2800 * time.Millisecond}
	for i := range want {
		if delays[i] != want[i] {
			t.Fatalf("delay[%d] = %s, want %s", i, delays[i], want[i])
		}
	}
	if got := delays[0] + delays[1] + delays[2]; got != 3*time.Second {
		t.Fatalf("burst cycle duration = %s, want 3s", got)
	}
}

func TestBurstProfileUsesConfiguredDelayDivisor(t *testing.T) {
	t.Parallel()
	profile, err := newWithRandomSource(Config{
		Type: TypeBurst, MinBurstSize: 3, MaxBurstSize: 3, DelayDivisor: 5,
	}, &sequenceRandomSource{values: []int64{0}})
	if err != nil {
		t.Fatalf("newWithRandomSource() error = %v", err)
	}

	if got := profile.NextDelay(delayContext(60, 1, 3)); got != 200*time.Millisecond {
		t.Fatalf("intra-burst delay = %s, want 200ms", got)
	}
	if got := profile.NextDelay(delayContext(60, 3, 3)); got != 2600*time.Millisecond {
		t.Fatalf("post-burst delay = %s, want 2.6s", got)
	}
}

func TestBurstProfileSelectsInclusiveConfiguredSize(t *testing.T) {
	t.Parallel()
	random := &sequenceRandomSource{values: []int64{2}}
	profile, err := newWithRandomSource(Config{
		Type: TypeBurst, MinBurstSize: 3, MaxBurstSize: 5,
	}, random)
	if err != nil {
		t.Fatalf("newWithRandomSource() error = %v", err)
	}
	batchSize := profile.BatchSize(60)
	if batchSize != 5 {
		t.Fatalf("BatchSize() = %d, want 5", batchSize)
	}

	for i := 0; i < 4; i++ {
		if got := profile.NextDelay(delayContext(60, i+1, batchSize)); got != 100*time.Millisecond {
			t.Fatalf("intra-burst delay[%d] = %s, want 100ms", i, got)
		}
	}
	if got := profile.NextDelay(delayContext(60, batchSize, batchSize)); got != 4600*time.Millisecond {
		t.Fatalf("post-burst delay = %s, want 4.6s", got)
	}
}

func TestBurstProfileCapsGroupAtCurrentRequestsPerMinute(t *testing.T) {
	t.Parallel()
	random := &sequenceRandomSource{values: []int64{0}}
	profile, err := newWithRandomSource(Config{
		Type: TypeBurst, MinBurstSize: 5, MaxBurstSize: 15,
	}, random)
	if err != nil {
		t.Fatalf("newWithRandomSource() error = %v", err)
	}
	batchSize := profile.BatchSize(2)
	if batchSize != 2 {
		t.Fatalf("BatchSize() = %d, want 2", batchSize)
	}

	if got := profile.NextDelay(delayContext(2, 1, batchSize)); got != 3*time.Second {
		t.Fatalf("intra-burst delay = %s, want 3s", got)
	}
	if got := profile.NextDelay(delayContext(2, batchSize, batchSize)); got != 57*time.Second {
		t.Fatalf("post-burst delay = %s, want 57s", got)
	}
}

func TestBurstProfileRestartsGroupWhenScheduleRateChanges(t *testing.T) {
	t.Parallel()
	random := &sequenceRandomSource{values: []int64{0, 0}}
	profile, err := newWithRandomSource(Config{
		Type: TypeBurst, MinBurstSize: 5, MaxBurstSize: 15,
	}, random)
	if err != nil {
		t.Fatalf("newWithRandomSource() error = %v", err)
	}

	if got := profile.BatchSize(128); got != 5 {
		t.Fatalf("high-rate BatchSize() = %d, want 5", got)
	}
	if got := profile.NextDelay(delayContext(128, 1, 5)); got != 46_875*time.Microsecond {
		t.Fatalf("high-rate intra-burst delay = %s, want 46.875ms", got)
	}
	if got := profile.BatchSize(2); got != 2 {
		t.Fatalf("changed-rate BatchSize() = %d, want 2", got)
	}
	if got := profile.NextDelay(delayContext(2, 1, 2)); got != 3*time.Second {
		t.Fatalf("changed-rate intra-burst delay = %s, want 3s", got)
	}
	if got := profile.NextDelay(delayContext(2, 2, 2)); got != 57*time.Second {
		t.Fatalf("changed-rate post-burst delay = %s, want 57s", got)
	}
}

func delayContext(requestsPerMinute, position, batchSize int) DelayContext {
	return DelayContext{
		RequestsPerMinute: requestsPerMinute,
		Position:          position,
		BatchSize:         batchSize,
	}
}
