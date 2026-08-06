package runner

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mykyta-kravchenko98/Kurama/internal/closeutil"
)

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

	values, err := e.resolveVariables(ctx, operation.Request.Variables)
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
	if err := applyRequestHeaders(request, operation.Request); err != nil {
		return result, fmt.Errorf("prepare operation %q request headers: %w", operation.Name, err)
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
		if err := e.stores.Put(ctx, operation.Capture.Store, value); err != nil {
			return result, fmt.Errorf("capture operation %q into store %q: %w", operation.Name, operation.Capture.Store, err)
		}
		result.Captured = true
	}
	return result, nil
}
