package ratelimit

import (
	"testing"
	"time"
)

func TestRateLimitValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		limit   Limit
		wantErr bool
	}{
		{name: "valid", limit: Limit{Requests: 30, Window: time.Minute}},
		{name: "zero requests", limit: Limit{Window: time.Minute}, wantErr: true},
		{name: "negative requests", limit: Limit{Requests: -1, Window: time.Minute}, wantErr: true},
		{name: "zero window", limit: Limit{Requests: 30}, wantErr: true},
		{name: "negative window", limit: Limit{Requests: 30, Window: -time.Second}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := test.limit.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestValidatePermits(t *testing.T) {
	t.Parallel()
	limit := Limit{Requests: 5, Window: time.Minute}
	tests := []struct {
		name    string
		permits int
		wantErr bool
	}{
		{name: "one", permits: 1},
		{name: "complete budget", permits: 5},
		{name: "zero", wantErr: true},
		{name: "negative", permits: -1, wantErr: true},
		{name: "above budget", permits: 6, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validatePermits(limit, test.permits)
			if (err != nil) != test.wantErr {
				t.Fatalf("validatePermits() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
