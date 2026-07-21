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
