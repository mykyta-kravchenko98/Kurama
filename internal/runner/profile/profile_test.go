package profile

import "testing"

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config Config
	}{
		{name: "unknown profile", config: Config{Type: "normal"}},
		{name: "fixed with burst fields", config: Config{Type: TypeFixed, MinBurstSize: 2}},
		{name: "uniform with burst fields", config: Config{Type: TypeUniform, MaxBurstSize: 5}},
		{name: "fixed with delay divisor", config: Config{Type: TypeFixed, DelayDivisor: 10}},
		{name: "burst below minimum", config: Config{Type: TypeBurst, MinBurstSize: 1, MaxBurstSize: 5}},
		{name: "burst inverted range", config: Config{Type: TypeBurst, MinBurstSize: 5, MaxBurstSize: 4}},
		{name: "burst delay divisor below minimum", config: Config{
			Type: TypeBurst, MinBurstSize: 2, MaxBurstSize: 5, DelayDivisor: MinimumBurstDelayDivisor - 1,
		}},
		{name: "burst delay divisor above maximum", config: Config{
			Type: TypeBurst, MinBurstSize: 2, MaxBurstSize: 5, DelayDivisor: MaximumBurstDelayDivisor + 1,
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(test.config); err == nil {
				t.Fatal("New() error = nil")
			}
		})
	}
}
