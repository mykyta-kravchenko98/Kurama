package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestExecutorPostCapturesShortURL(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/data/shorten" {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if got := body["longURL"]; got != "https://example.invalid/kurama/fixed-id" {
			t.Errorf("longURL = %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"shortURL":"abc123"}`))
	}))
	defer server.Close()

	store := newFakeValueStore()
	executor := newTestExecutor(t, server, store)
	operation := validConfig().Operations[0]
	body, err := json.Marshal(map[string]string{"longURL": "https://example.invalid/kurama/{{id}}"})
	if err != nil {
		t.Fatal(err)
	}
	operation.Request.BodyTemplate = string(body)

	result, err := executor.Execute(context.Background(), operation)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.StatusCode != http.StatusOK || !result.Captured || result.Duration <= 0 {
		t.Fatalf("result = %#v", result)
	}
	if got, ok, err := store.Random(context.Background(), "hashes"); err != nil || !ok || got != "abc123" {
		t.Fatalf("captured hash = %q, %v, %v", got, ok, err)
	}
}

func TestExecutorDoesNotFollowRedirect(t *testing.T) {
	t.Parallel()
	var followed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/followed" {
			followed.Add(1)
			writer.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(writer, request, "/followed", http.StatusPermanentRedirect)
	}))
	defer server.Close()

	executor := newTestExecutor(t, server, nil)
	operation := OperationConfig{
		Name: "redirect", Weight: 1,
		Request:             RequestConfig{Method: http.MethodGet, PathTemplate: "/redirect"},
		ExpectedStatusCodes: []int{http.StatusPermanentRedirect},
	}
	result, err := executor.Execute(context.Background(), operation)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.StatusCode != http.StatusPermanentRedirect || followed.Load() != 0 {
		t.Fatalf("status = %d, followed = %d", result.StatusCode, followed.Load())
	}
}

func TestExecutorRejectsOversizedResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(bytes.Repeat([]byte{'x'}, MaxResponseBodyBytes+1))
	}))
	defer server.Close()

	executor := newTestExecutor(t, server, nil)
	_, err := executor.Execute(context.Background(), simpleGETOperation(http.StatusOK))
	if !errors.Is(err, ErrResponseBodyTooLarge) {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestExecutorReportsUnexpectedStatus(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	executor := newTestExecutor(t, server, nil)
	result, err := executor.Execute(context.Background(), simpleGETOperation(http.StatusOK))
	if !errors.Is(err, ErrUnexpectedStatus) || result.StatusCode != http.StatusInternalServerError {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestExecutorSkipsRequestWhenStoreIsEmpty(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	executor := newTestExecutor(t, server, newFakeValueStore())
	_, err := executor.Execute(context.Background(), validConfig().Operations[1])
	if !errors.Is(err, ErrStoreValueUnavailable) || requests.Load() != 0 {
		t.Fatalf("error = %v, requests = %d", err, requests.Load())
	}
}

func TestExecutorEscapesPathVariable(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.RequestURI != "/api/v1/a%2Fb%3Fc" {
			t.Errorf("RequestURI = %q", request.RequestURI)
		}
		writer.WriteHeader(http.StatusPermanentRedirect)
	}))
	defer server.Close()

	store := newFakeValueStore()
	_ = store.Put(context.Background(), "hashes", "a/b?c")
	executor := newTestExecutor(t, server, store)
	_, err := executor.Execute(context.Background(), validConfig().Operations[1])
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
}

func TestCaptureJSONStringSupportsArrayAndEscapedTokens(t *testing.T) {
	t.Parallel()
	value, err := captureJSONString([]byte(`{"items":[{"a/b":"ok"}]}`), "/items/0/a~1b")
	if err != nil {
		t.Fatalf("captureJSONString() error: %v", err)
	}
	if value != "ok" {
		t.Fatalf("value = %q", value)
	}
}

func newTestExecutor(t *testing.T, server *httptest.Server, store ValueStore) *Executor {
	t.Helper()
	executor, err := NewExecutor(
		TargetConfig{BaseURL: server.URL},
		store,
		WithHTTPClient(server.Client()),
		WithValueGenerator(fixedValueGenerator{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func simpleGETOperation(expectedStatus int) OperationConfig {
	return OperationConfig{
		Name: "get", Weight: 1,
		Request:             RequestConfig{Method: http.MethodGet, PathTemplate: "/get"},
		ExpectedStatusCodes: []int{expectedStatus},
	}
}

type fakeValueStore struct {
	mu     sync.RWMutex
	values map[string][]string
}

func newFakeValueStore() *fakeValueStore {
	return &fakeValueStore{values: map[string][]string{}}
}

func (s *fakeValueStore) Random(_ context.Context, store string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := s.values[store]
	if len(values) == 0 {
		return "", false, nil
	}
	return values[len(values)-1], true, nil
}

func (s *fakeValueStore) Put(_ context.Context, store, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[store] = append(s.values[store], value)
	return nil
}

type fixedValueGenerator struct{}

func (fixedValueGenerator) UUID() (string, error) { return "fixed-id", nil }
func (fixedValueGenerator) Base62(length int) (string, error) {
	return strings.Repeat("Z", length), nil
}
