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

func TestConfigValidateRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "non HTTP target", mutate: func(c *Config) { c.Target.BaseURL = "postgres://db" }, wantErr: "scheme"},
		{name: "credentials in target", mutate: func(c *Config) { c.Target.BaseURL = "http://user:pass@shorturl:8585" }, wantErr: "credentials"},
		{name: "zero rate", mutate: func(c *Config) { c.Rate.RequestsPerMinute = 0 }, wantErr: "requestsPerMinute"},
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
	data, err := json.Marshal(validConfig())
	if err != nil {
		t.Fatal(err)
	}
	config, err := DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeConfig() error: %v", err)
	}
	if config.Rate.RequestsPerMinute != 30 {
		t.Fatalf("requestsPerMinute = %d", config.Rate.RequestsPerMinute)
	}
}

func TestDecodeConfigRejectsInvalidDocuments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "unknown field", input: `{"target":{"baseURL":"http://shorturl:8585"},"rate":{"requestsPerMinute":30},"operations":[],"typo":true}`, wantErr: "unknown field"},
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
		Rate:   RateConfig{RequestsPerMinute: 30},
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
