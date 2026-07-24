package runner

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestConfigValidateShortURLScenario(t *testing.T) {
	t.Parallel()
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("valid ShortUrl config rejected: %v", err)
	}
}

func TestConfigValidateUniformSchedule(t *testing.T) {
	t.Parallel()
	config := validConfig()
	config.Rate.Schedule = RateScheduleConfig{
		Type: "uniform", MinRequestsPerMinute: 2, MaxRequestsPerMinute: 56, WindowMinutes: 1,
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("valid uniform schedule rejected: %v", err)
	}
}

func TestConfigValidateBurstProfile(t *testing.T) {
	t.Parallel()
	config := validConfig()
	config.Rate.Profile = &RateProfileConfig{
		Type: "burst", MinBurstSize: 5, MaxBurstSize: 15, DelayDivisor: 10,
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("valid burst profile rejected: %v", err)
	}
}

func TestConfigValidateRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "non HTTP target", mutate: func(c *Config) { c.Target.BaseURL = "postgres://db" }, wantErr: "scheme"},
		{name: "credentials in target", mutate: func(c *Config) { c.Target.BaseURL = "http://user:pass@shorturl:8585" }, wantErr: "credentials"},
		{name: "zero fixed rate", mutate: func(c *Config) { c.Rate.Schedule.RequestsPerMinute = 0 }, wantErr: "requestsPerMinute"},
		{name: "unknown schedule", mutate: func(c *Config) { c.Rate.Schedule.Type = "burst" }, wantErr: "schedule.type"},
		{name: "fixed with uniform fields", mutate: func(c *Config) { c.Rate.Schedule.MinRequestsPerMinute = 2 }, wantErr: "must not set uniform"},
		{name: "uniform with fixed field", mutate: func(c *Config) {
			c.Rate.Schedule = RateScheduleConfig{Type: "uniform", RequestsPerMinute: 30, MinRequestsPerMinute: 2, MaxRequestsPerMinute: 56, WindowMinutes: 1}
		}, wantErr: "must not set requestsPerMinute"},
		{name: "uniform invalid range", mutate: func(c *Config) {
			c.Rate.Schedule = RateScheduleConfig{Type: "uniform", MinRequestsPerMinute: 56, MaxRequestsPerMinute: 2, WindowMinutes: 1}
		}, wantErr: "maxRequestsPerMinute"},
		{name: "uniform zero window", mutate: func(c *Config) {
			c.Rate.Schedule = RateScheduleConfig{Type: "uniform", MinRequestsPerMinute: 2, MaxRequestsPerMinute: 56}
		}, wantErr: "windowMinutes"},
		{name: "unknown limiter", mutate: func(c *Config) {
			c.Rate.Limiter = &RateLimiterConfig{Type: "postgres"}
		}, wantErr: "rate.limiter.type"},
		{name: "unknown profile", mutate: func(c *Config) {
			c.Rate.Profile = &RateProfileConfig{Type: "normal"}
		}, wantErr: "rate.profile.type"},
		{name: "fixed profile with burst fields", mutate: func(c *Config) {
			c.Rate.Profile = &RateProfileConfig{Type: "fixed", MinBurstSize: 5}
		}, wantErr: "must not set burst"},
		{name: "uniform profile with burst fields", mutate: func(c *Config) {
			c.Rate.Profile = &RateProfileConfig{Type: "uniform", MaxBurstSize: 15}
		}, wantErr: "must not set burst"},
		{name: "fixed profile with delay divisor", mutate: func(c *Config) {
			c.Rate.Profile = &RateProfileConfig{Type: "fixed", DelayDivisor: 10}
		}, wantErr: "must not set burst"},
		{name: "burst profile below minimum", mutate: func(c *Config) {
			c.Rate.Profile = &RateProfileConfig{Type: "burst", MinBurstSize: 1, MaxBurstSize: 15}
		}, wantErr: "minBurstSize"},
		{name: "burst profile inverted range", mutate: func(c *Config) {
			c.Rate.Profile = &RateProfileConfig{Type: "burst", MinBurstSize: 15, MaxBurstSize: 5}
		}, wantErr: "maxBurstSize"},
		{name: "burst profile above maximum", mutate: func(c *Config) {
			c.Rate.Profile = &RateProfileConfig{Type: "burst", MinBurstSize: 5, MaxBurstSize: MaxProfileBurstSize + 1}
		}, wantErr: "maxBurstSize"},
		{name: "burst profile delay divisor below minimum", mutate: func(c *Config) {
			c.Rate.Profile = &RateProfileConfig{Type: "burst", MinBurstSize: 5, MaxBurstSize: 15, DelayDivisor: 1}
		}, wantErr: "delayDivisor"},
		{name: "burst profile delay divisor above maximum", mutate: func(c *Config) {
			c.Rate.Profile = &RateProfileConfig{
				Type: "burst", MinBurstSize: 5, MaxBurstSize: 15, DelayDivisor: MaxProfileDelayDivisor + 1,
			}
		}, wantErr: "delayDivisor"},
		{name: "duplicate store", mutate: func(c *Config) { c.Stores = append(c.Stores, c.Stores[0]) }, wantErr: "duplicated"},
		{name: "oversized body", mutate: func(c *Config) { c.Operations[0].Request.BodyTemplate = strings.Repeat("x", MaxRequestBodyBytes+1) }, wantErr: "exceeds"},
		{name: "GET body", mutate: func(c *Config) { c.Operations[1].Request.BodyTemplate = "{}" }, wantErr: "GET request"},
		{name: "undefined variable", mutate: func(c *Config) { c.Operations[1].Request.PathTemplate = "/api/v1/{{missing}}" }, wantErr: "undefined variable"},
		{name: "malformed variable", mutate: func(c *Config) { c.Operations[1].Request.PathTemplate = "/api/v1/{{hash}" }, wantErr: "malformed variable"},
		{name: "unused variable", mutate: func(c *Config) { c.Operations[1].Request.PathTemplate = "/api/v1/static" }, wantErr: "defined but unused"},
		{name: "unknown store", mutate: func(c *Config) { c.Operations[1].Request.Variables[0].Source.Store = "missing" }, wantErr: "undeclared store"},
		{name: "invalid capture pointer", mutate: func(c *Config) { c.Operations[0].Capture.JSONPointer = "shortURL" }, wantErr: "begin with slash"},
		{name: "invalid pointer escape", mutate: func(c *Config) { c.Operations[0].Capture.JSONPointer = "/short~URL" }, wantErr: "invalid RFC 6901"},
		{name: "invalid status", mutate: func(c *Config) { c.Operations[0].ExpectedStatusCodes = []int{700} }, wantErr: "invalid HTTP status"},
		{name: "transport header", mutate: func(c *Config) { c.Operations[0].Request.Headers["Host"] = "other.test" }, wantErr: "transport header"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := validConfig()
			test.mutate(&config)
			err := config.Validate()
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestDecodeConfig(t *testing.T) {
	t.Parallel()
	want := validConfig()
	want.Rate.Limiter = &RateLimiterConfig{Type: "redis"}
	want.Rate.Profile = &RateProfileConfig{Type: "uniform"}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	config, err := DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeConfig() error: %v", err)
	}
	if config.Rate.Schedule.Type != "fixed" || config.Rate.Schedule.RequestsPerMinute != 30 {
		t.Fatalf("rate schedule = %#v", config.Rate.Schedule)
	}
	if config.Rate.Limiter == nil || config.Rate.Limiter.Type != "redis" {
		t.Fatalf("rate limiter = %#v, want redis", config.Rate.Limiter)
	}
	if config.Rate.Profile == nil || config.Rate.Profile.Type != "uniform" {
		t.Fatalf("rate profile = %#v, want uniform", config.Rate.Profile)
	}
}

func TestDecodeConfigRejectsInvalidDocuments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "unknown field", input: `{"target":{"baseURL":"http://shorturl:8585"},"rate":{"schedule":{"type":"fixed","requestsPerMinute":30}},"operations":[],"typo":true}`, wantErr: "unknown field"},
		{name: "trailing document", input: `{}` + "\n" + `{}`, wantErr: "multiple JSON documents"},
		{name: "oversized config", input: strings.Repeat(" ", MaxConfigBytes+1), wantErr: "config exceeds"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := DecodeConfig(strings.NewReader(test.input))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("DecodeConfig() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func validConfig() Config {
	return Config{
		Target: TargetConfig{BaseURL: "http://shorturl:8585"},
		Rate: RateConfig{Schedule: RateScheduleConfig{
			Type: "fixed", RequestsPerMinute: 30,
		}},
		Stores: []StoreConfig{{Name: "hashes", Capacity: 10_000}},
		Operations: []OperationConfig{
			{
				Name:   "create",
				Weight: 20,
				Request: RequestConfig{
					Method:       "POST",
					PathTemplate: "/api/v1/data/shorten",
					Headers:      map[string]string{"Content-Type": "application/json"},
					BodyTemplate: `{"longURL":"https://example.invalid/kurama/{{id}}"}`,
					Variables:    []VariableConfig{{Name: "id", Source: VariableSource{Type: "randomUUID"}}},
				},
				ExpectedStatusCodes: []int{200},
				Capture:             &CaptureConfig{JSONPointer: "/shortURL", Store: "hashes"},
			},
			{
				Name:   "resolve-valid",
				Weight: 70,
				Request: RequestConfig{
					Method:       "GET",
					PathTemplate: "/api/v1/{{hash}}",
					Variables:    []VariableConfig{{Name: "hash", Source: VariableSource{Type: "store", Store: "hashes"}}},
				},
				ExpectedStatusCodes: []int{308},
			},
			{
				Name:   "resolve-invalid",
				Weight: 10,
				Request: RequestConfig{
					Method:       "GET",
					PathTemplate: "/api/v1/{{hash}}",
					Variables:    []VariableConfig{{Name: "hash", Source: VariableSource{Type: "randomBase62", Length: 8}}},
				},
				ExpectedStatusCodes: []int{404},
			},
		},
	}
}
