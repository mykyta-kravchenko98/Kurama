package runner

import (
	"testing"
	"time"
)

func TestRateLimitValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		limit   RateLimit
		wantErr bool
	}{
		{name: "valid", limit: RateLimit{Requests: 30, Window: time.Minute}},
		{name: "zero requests", limit: RateLimit{Window: time.Minute}, wantErr: true},
		{name: "negative requests", limit: RateLimit{Requests: -1, Window: time.Minute}, wantErr: true},
		{name: "zero window", limit: RateLimit{Requests: 30}, wantErr: true},
		{name: "negative window", limit: RateLimit{Requests: 30, Window: -time.Second}, wantErr: true},
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
