package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
)

func TestRunExecutesConfiguredWorkload(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Method != http.MethodGet || request.URL.Path != "/health" {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := runner.Config{
		Target: runner.TargetConfig{BaseURL: server.URL},
		Rate:   runner.RateConfig{RequestsPerMinute: 1},
		Operations: []runner.OperationConfig{{
			Name: "health", Weight: 1,
			Request:             runner.RequestConfig{Method: http.MethodGet, PathTemplate: "/health"},
			ExpectedStatusCodes: []int{http.StatusOK},
		}},
	}
	path := writeTestConfig(t, config)
	if err := run(ctx, path, runner.WithExecutionHandler(func(result runner.ExecutionResult, err error) {
		if err != nil || result.StatusCode != http.StatusOK {
			t.Errorf("execution result = %#v, error = %v", result, err)
		}
		cancel()
	})); err != nil {
		t.Fatalf("run() error: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d; want 1", got)
	}
}

func TestLoadConfigReportsInvalidConfig(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(path, []byte(`{"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("loadConfig() error = nil")
	}
}

func TestLoadConfigReportsMissingFile(t *testing.T) {
	t.Parallel()
	if _, err := loadConfig(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("loadConfig() error = nil")
	}
}

func writeTestConfig(t *testing.T, config runner.Config) string {
	t.Helper()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
