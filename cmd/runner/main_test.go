package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner/ratelimit"
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
	if err := runWithMetricsAddress(ctx, path, "127.0.0.1:0", runner.WithExecutionHandler(func(result runner.ExecutionResult, err error) {
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

func TestNewRuntimeStateDefaultsToMemory(t *testing.T) {
	t.Parallel()

	state, err := newRuntimeState(context.Background(), storeSettings{}, "local", []runner.StoreConfig{{Name: "hashes", Capacity: 1}})
	if err != nil {
		t.Fatalf("newRuntimeState() error = %v", err)
	}
	defer func() {
		if err := state.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()
	if err := state.Put(context.Background(), "hashes", "memory-value"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if value, ok, err := state.Random(context.Background(), "hashes"); err != nil || !ok || value != "memory-value" {
		t.Fatalf("Random() = (%q, %t, %v), want memory-value, true, nil", value, ok, err)
	}
	if _, ok := state.Limiter.(*ratelimit.LocalLimiter); !ok {
		t.Fatalf("limiter type = %T; want *ratelimit.LocalLimiter", state.Limiter)
	}
	assertLimiterAllowsOneRequest(t, state.Limiter)
}

func TestNewRuntimeStateCreatesRedisBackend(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	state, err := newRuntimeState(context.Background(), storeSettings{
		Backend:      "redis",
		RedisAddress: server.Addr(),
		Namespace:    "shorturl",
		Scenario:     "load",
	}, "redis", []runner.StoreConfig{{Name: "hashes", Capacity: 1}})
	if err != nil {
		t.Fatalf("newRuntimeState() error = %v", err)
	}
	defer func() {
		if err := state.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()
	if err := state.Put(context.Background(), "hashes", "redis-value"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	values, err := server.List("kurama:v1:shorturl:load:hashes")
	if err != nil {
		t.Fatalf("read Redis list: %v", err)
	}
	if len(values) != 1 || values[0] != "redis-value" {
		t.Fatalf("Redis values = %v, want [redis-value]", values)
	}
	if _, ok := state.Limiter.(*ratelimit.RedisRateLimiter); !ok {
		t.Fatalf("limiter type = %T; want *ratelimit.RedisRateLimiter", state.Limiter)
	}
	assertLimiterAllowsOneRequest(t, state.Limiter)
}

func TestNewRuntimeStateSupportsMemoryStoreWithRedisLimiter(t *testing.T) {
	t.Parallel()
	server := miniredis.RunT(t)
	state, err := newRuntimeState(context.Background(), storeSettings{
		RedisAddress: server.Addr(),
		Namespace:    "shorturl",
		Scenario:     "load",
	}, "redis", []runner.StoreConfig{{Name: "hashes", Capacity: 1}})
	if err != nil {
		t.Fatalf("newRuntimeState() error = %v", err)
	}
	defer func() {
		if err := state.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()
	if _, ok := state.ValueStore.(*runner.MemoryStore); !ok {
		t.Fatalf("store type = %T; want *runner.MemoryStore", state.ValueStore)
	}
	if _, ok := state.Limiter.(*ratelimit.RedisRateLimiter); !ok {
		t.Fatalf("limiter type = %T; want *ratelimit.RedisRateLimiter", state.Limiter)
	}
}

func TestNormalizedRateLimiterBackend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		config       *runner.RateLimiterConfig
		storeBackend string
		want         string
	}{
		{name: "defaults to local for memory store", want: "local"},
		{name: "inherits Redis store backend", storeBackend: "redis", want: "redis"},
		{name: "explicit local overrides Redis store", config: &runner.RateLimiterConfig{Type: "local"}, storeBackend: "redis", want: "local"},
		{name: "explicit Redis with memory store", config: &runner.RateLimiterConfig{Type: "redis"}, want: "redis"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizedRateLimiterBackend(test.config, test.storeBackend); got != test.want {
				t.Fatalf("normalizedRateLimiterBackend() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizedRateProfileType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config *runner.RateProfileConfig
		want   string
	}{
		{name: "omitted", want: "fixed"},
		{name: "empty", config: &runner.RateProfileConfig{}, want: "fixed"},
		{name: "uniform", config: &runner.RateProfileConfig{Type: "uniform"}, want: "uniform"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizedRateProfileType(test.config); got != test.want {
				t.Fatalf("normalizedRateProfileType() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNewRuntimeStateRejectsInvalidBackendSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		settings       storeSettings
		limiterBackend string
	}{
		{name: "unknown storage backend", settings: storeSettings{Backend: "postgres"}, limiterBackend: "local"},
		{name: "unknown limiter backend", limiterBackend: "postgres"},
		{name: "missing Redis address", settings: storeSettings{Backend: "redis", Namespace: "shorturl", Scenario: "load"}, limiterBackend: "redis"},
		{name: "missing namespace", settings: storeSettings{Backend: "redis", RedisAddress: "redis:6379", Scenario: "load"}, limiterBackend: "redis"},
		{name: "missing scenario", settings: storeSettings{Backend: "redis", RedisAddress: "redis:6379", Namespace: "shorturl"}, limiterBackend: "redis"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := newRuntimeState(context.Background(), test.settings, test.limiterBackend, nil); err == nil {
				t.Fatal("newRuntimeState() error = nil")
			}
		})
	}
}

func assertLimiterAllowsOneRequest(t *testing.T, limiter ratelimit.Limiter) {
	t.Helper()
	decision, err := limiter.TryAcquire(context.Background(), ratelimit.Limit{Requests: 1, Window: time.Minute})
	if err != nil {
		t.Fatalf("TryAcquire() error = %v", err)
	}
	if !decision.Allowed {
		t.Fatal("first rate limit acquisition was rejected")
	}
}

func TestMetricsServerExportsRunnerMetricsAndShutsDown(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	observer, err := runner.NewPrometheusStoreObserver(registry)
	if err != nil {
		t.Fatalf("NewPrometheusStoreObserver() error = %v", err)
	}
	underlying, err := runner.NewMemoryStore([]runner.StoreConfig{{Name: "hashes", Capacity: 1}})
	if err != nil {
		t.Fatalf("NewMemoryStore() error = %v", err)
	}
	store, err := runner.NewInstrumentedStore(underlying, "memory", observer)
	if err != nil {
		t.Fatalf("NewInstrumentedStore() error = %v", err)
	}
	if err := store.Put(context.Background(), "hashes", "value"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	limiterObserver, err := ratelimit.NewPrometheusObserver(registry)
	if err != nil {
		t.Fatalf("NewPrometheusObserver() error = %v", err)
	}
	limiter, err := ratelimit.NewInstrumentedLimiter(ratelimit.NewLocalLimiter(), "memory", limiterObserver)
	if err != nil {
		t.Fatalf("NewInstrumentedLimiter() error = %v", err)
	}
	if _, err := limiter.TryAcquire(context.Background(), ratelimit.Limit{Requests: 1, Window: time.Minute}); err != nil {
		t.Fatalf("TryAcquire() error = %v", err)
	}

	server, err := startMetricsServer("127.0.0.1:0", registry)
	if err != nil {
		t.Fatalf("startMetricsServer() error = %v", err)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://" + server.address + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		t.Fatalf("read /metrics response: %v", readErr)
	}
	if closeErr != nil {
		t.Fatalf("close /metrics response: %v", closeErr)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics status = %d", response.StatusCode)
	}
	if !strings.Contains(string(body), `kurama_store_operations_total{backend="memory",operation="put",result="success",store="hashes"} 1`) {
		t.Fatalf("/metrics response does not contain store counter:\n%s", body)
	}
	if !strings.Contains(string(body), `kurama_rate_limiter_acquisitions_total{backend="memory",result="allowed"} 1`) {
		t.Fatalf("/metrics response does not contain rate limiter counter:\n%s", body)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
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
