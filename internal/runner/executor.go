package runner

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mykyta-kravchenko98/Kurama/internal/closeutil"
)

const (
	DefaultRequestTimeout = 10 * time.Second
	MaxCapturedValueBytes = 4 << 10
	base62Alphabet        = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

var (
	ErrStoreValueUnavailable = errors.New("store value unavailable")
	ErrUnexpectedStatus      = errors.New("unexpected HTTP status")
	ErrResponseBodyTooLarge  = errors.New("response body too large")
)

// ValueStore is the executor-facing boundary for scenario state.
type ValueStore interface {
	Random(store string) (string, bool)
	Put(store, value string) error
}

// ValueGenerator produces template values. It is injectable so executor tests
// are deterministic without weakening production randomness.
type ValueGenerator interface {
	UUID() (string, error)
	Base62(length int) (string, error)
}

type Executor struct {
	baseURL   *url.URL
	client    *http.Client
	stores    ValueStore
	generator ValueGenerator
}

type ExecutorOption func(*Executor)

func WithHTTPClient(client *http.Client) ExecutorOption {
	return func(executor *Executor) {
		if client != nil {
			executor.client = cloneHTTPClient(client)
		}
	}
}

func WithValueGenerator(generator ValueGenerator) ExecutorOption {
	return func(executor *Executor) {
		if generator != nil {
			executor.generator = generator
		}
	}
}

func NewExecutor(target TargetConfig, stores ValueStore, options ...ExecutorOption) (*Executor, error) {
	if err := validateTarget(target); err != nil {
		return nil, err
	}
	baseURL, err := url.Parse(target.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse target.baseURL: %w", err)
	}
	executor := &Executor{
		baseURL:   baseURL,
		client:    cloneHTTPClient(&http.Client{Timeout: DefaultRequestTimeout}),
		stores:    stores,
		generator: cryptoValueGenerator{},
	}
	for _, option := range options {
		option(executor)
	}
	return executor, nil
}

func cloneHTTPClient(source *http.Client) *http.Client {
	clone := *source
	if clone.Timeout == 0 {
		clone.Timeout = DefaultRequestTimeout
	}
	// A generated short URL deliberately points outside the cluster. Kurama is
	// measuring the target API's redirect response, not following that URL.
	clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone
}

type ExecutionResult struct {
	Operation     string
	StatusCode    int
	ResponseBytes int
	Duration      time.Duration
	Captured      bool
}

func (e *Executor) Execute(ctx context.Context, operation OperationConfig) (result ExecutionResult, resultErr error) {
	started := time.Now()
	result = ExecutionResult{Operation: operation.Name}
	defer func() { result.Duration = time.Since(started) }()

	values, err := e.resolveVariables(operation.Request.Variables)
	if err != nil {
		return result, fmt.Errorf("resolve variables for operation %q: %w", operation.Name, err)
	}
	path := renderPathTemplate(operation.Request.PathTemplate, values)
	body := renderBodyTemplate(operation.Request.BodyTemplate, values)
	if len(body) > MaxRequestBodyBytes {
		return result, fmt.Errorf("render operation %q: request body exceeds %d bytes", operation.Name, MaxRequestBodyBytes)
	}

	requestURL, err := e.resolveRequestURL(path)
	if err != nil {
		return result, fmt.Errorf("render operation %q URL: %w", operation.Name, err)
	}
	request, err := http.NewRequestWithContext(ctx, operation.Request.Method, requestURL.String(), strings.NewReader(body))
	if err != nil {
		return result, fmt.Errorf("create operation %q request: %w", operation.Name, err)
	}
	for name, value := range operation.Request.Headers {
		request.Header.Set(name, value)
	}

	response, err := e.client.Do(request)
	if err != nil {
		return result, fmt.Errorf("execute operation %q: %w", operation.Name, err)
	}
	defer closeutil.Close(ctx, response.Body)
	result.StatusCode = response.StatusCode

	responseBody, err := readBoundedResponse(response.Body)
	if err != nil {
		return result, fmt.Errorf("execute operation %q: %w", operation.Name, err)
	}
	result.ResponseBytes = len(responseBody)
	if !containsStatus(operation.ExpectedStatusCodes, response.StatusCode) {
		return result, fmt.Errorf("%w for operation %q: got %d, expected one of %v",
			ErrUnexpectedStatus, operation.Name, response.StatusCode, operation.ExpectedStatusCodes)
	}

	if operation.Capture != nil {
		if e.stores == nil {
			return result, fmt.Errorf("capture operation %q: no value store configured", operation.Name)
		}
		value, err := captureJSONString(responseBody, operation.Capture.JSONPointer)
		if err != nil {
			return result, fmt.Errorf("capture operation %q response: %w", operation.Name, err)
		}
		if err := e.stores.Put(operation.Capture.Store, value); err != nil {
			return result, fmt.Errorf("capture operation %q into store %q: %w", operation.Name, operation.Capture.Store, err)
		}
		result.Captured = true
	}
	return result, nil
}

func (e *Executor) resolveVariables(variables []VariableConfig) (map[string]string, error) {
	values := make(map[string]string, len(variables))
	for _, variable := range variables {
		var value string
		var err error
		switch variable.Source.Type {
		case "randomUUID":
			value, err = e.generator.UUID()
		case "randomBase62":
			value, err = e.generator.Base62(variable.Source.Length)
		case "store":
			if e.stores == nil {
				return nil, fmt.Errorf("%w: store %q is not configured", ErrStoreValueUnavailable, variable.Source.Store)
			}
			var ok bool
			value, ok = e.stores.Random(variable.Source.Store)
			if !ok {
				return nil, fmt.Errorf("%w: store %q is empty", ErrStoreValueUnavailable, variable.Source.Store)
			}
		default:
			return nil, fmt.Errorf("unsupported variable source %q", variable.Source.Type)
		}
		if err != nil {
			return nil, fmt.Errorf("generate variable %q: %w", variable.Name, err)
		}
		values[variable.Name] = value
	}
	return values, nil
}

func (e *Executor) resolveRequestURL(path string) (*url.URL, error) {
	reference, err := url.Parse(path)
	if err != nil {
		return nil, err
	}
	resolved := e.baseURL.ResolveReference(reference)
	if resolved.Scheme != e.baseURL.Scheme || resolved.Host != e.baseURL.Host {
		return nil, fmt.Errorf("rendered path attempted to override target origin")
	}
	return resolved, nil
}

func readBoundedResponse(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, MaxResponseBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if len(body) > MaxResponseBodyBytes {
		return nil, fmt.Errorf("%w: limit is %d bytes", ErrResponseBodyTooLarge, MaxResponseBodyBytes)
	}
	return body, nil
}

func containsStatus(expected []int, actual int) bool {
	for _, status := range expected {
		if status == actual {
			return true
		}
	}
	return false
}

func renderPathTemplate(template string, values map[string]string) string {
	return replaceTemplateVariables(template, values, url.PathEscape)
}

func renderBodyTemplate(template string, values map[string]string) string {
	return replaceTemplateVariables(template, values, escapeJSONStringContent)
}

func replaceTemplateVariables(template string, values map[string]string, escape func(string) string) string {
	return templateVarPattern.ReplaceAllStringFunc(template, func(match string) string {
		name := strings.TrimSpace(match[2 : len(match)-2])
		return escape(values[name])
	})
}

func escapeJSONStringContent(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded[1 : len(encoded)-1])
}

func captureJSONString(body []byte, pointer string) (string, error) {
	var document any
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return "", fmt.Errorf("decode JSON: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return "", err
	}
	value, err := resolveJSONPointer(document, pointer)
	if err != nil {
		return "", err
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("JSON pointer %q resolved to %T, want string", pointer, value)
	}
	if text == "" {
		return "", fmt.Errorf("JSON pointer %q resolved to an empty string", pointer)
	}
	if len(text) > MaxCapturedValueBytes {
		return "", fmt.Errorf("captured value exceeds %d bytes", MaxCapturedValueBytes)
	}
	return text, nil
}

func resolveJSONPointer(document any, pointer string) (any, error) {
	current := document
	for _, encodedToken := range strings.Split(pointer[1:], "/") {
		token := strings.ReplaceAll(strings.ReplaceAll(encodedToken, "~1", "/"), "~0", "~")
		switch container := current.(type) {
		case map[string]any:
			next, exists := container[token]
			if !exists {
				return nil, fmt.Errorf("JSON pointer %q does not exist", pointer)
			}
			current = next
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(container) {
				return nil, fmt.Errorf("JSON pointer %q contains invalid array index %q", pointer, token)
			}
			current = container[index]
		default:
			return nil, fmt.Errorf("JSON pointer %q encountered %T before token %q", pointer, current, token)
		}
	}
	return current, nil
}

type cryptoValueGenerator struct{}

func (cryptoValueGenerator) UUID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		data[0:4], data[4:6], data[6:8], data[8:10], data[10:16]), nil
}

func (cryptoValueGenerator) Base62(length int) (string, error) {
	var result strings.Builder
	result.Grow(length)
	max := big.NewInt(int64(len(base62Alphabet)))
	for range length {
		index, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		result.WriteByte(base62Alphabet[index.Int64()])
	}
	return result.String(), nil
}
