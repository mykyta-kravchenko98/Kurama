package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	// MaxConfigBytes stays within the practical size of a ConfigMap-backed
	// scenario and prevents an unexpectedly large local file from being read.
	MaxConfigBytes = 1 << 20
	// MaxRequestBodyBytes bounds a rendered request body. Phase 2 targets
	// ordinary JSON APIs rather than file uploads or multipart requests.
	MaxRequestBodyBytes = 64 << 10
	// MaxResponseBodyBytes bounds response data retained for validation and
	// capture. The executor will read one additional byte to detect overflow.
	MaxResponseBodyBytes = 1 << 20

	MaxRequestsPerMinute = 6_000
	MaxOperations        = 64
	MaxStores            = 32
	MaxStoreCapacity     = 100_000
)

var (
	namePattern        = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	templateVarPattern = regexp.MustCompile(`\{\{([^{}]*)\}\}`)
)

// Config is the version-independent contract consumed by a runner Pod. It
// contains no Kubernetes types so execution can be tested with httptest and
// reused outside a controller process.
type Config struct {
	Target     TargetConfig      `json:"target"`
	Rate       RateConfig        `json:"rate"`
	Stores     []StoreConfig     `json:"stores,omitempty"`
	Operations []OperationConfig `json:"operations"`
}

type TargetConfig struct {
	BaseURL string `json:"baseURL"`
}

type RateConfig struct {
	RequestsPerMinute int                `json:"requestsPerMinute"`
	Limiter           *RateLimiterConfig `json:"limiter,omitempty"`
	Profile           *RateProfileConfig `json:"profile,omitempty"`
}

type RateLimiterConfig struct {
	Type string `json:"type,omitempty"`
}

type RateProfileConfig struct {
	Type string `json:"type,omitempty"`
}

type StoreConfig struct {
	Name     string `json:"name"`
	Capacity int    `json:"capacity"`
}

type OperationConfig struct {
	Name                string         `json:"name"`
	Weight              int            `json:"weight"`
	Request             RequestConfig  `json:"request"`
	ExpectedStatusCodes []int          `json:"expectedStatusCodes"`
	Capture             *CaptureConfig `json:"capture,omitempty"`
}

type RequestConfig struct {
	Method       string            `json:"method"`
	PathTemplate string            `json:"pathTemplate"`
	Headers      map[string]string `json:"headers,omitempty"`
	BodyTemplate string            `json:"bodyTemplate,omitempty"`
	Variables    []VariableConfig  `json:"variables,omitempty"`
}

type VariableConfig struct {
	Name   string         `json:"name"`
	Source VariableSource `json:"source"`
}

type VariableSource struct {
	// Type is one of randomUUID, randomBase62 or store.
	Type string `json:"type"`
	// Store is required only for type=store.
	Store string `json:"store,omitempty"`
	// Length is required only for type=randomBase62.
	Length int `json:"length,omitempty"`
}

type CaptureConfig struct {
	// JSONPointer is an RFC 6901 pointer into a JSON response, for example
	// /shortURL. Capturing the entire document is not supported in Phase 2.
	JSONPointer string `json:"jsonPointer"`
	Store       string `json:"store"`
}

// DecodeConfig reads one strict JSON document and validates it. Unknown fields
// and trailing JSON are rejected so a misspelled scenario field cannot silently
// result in a different load profile.
func DecodeConfig(reader io.Reader) (Config, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxConfigBytes+1))
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if len(data) > MaxConfigBytes {
		return Config{}, fmt.Errorf("config exceeds %d bytes", MaxConfigBytes)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Config{}, err
	}
	if err := config.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
	}
	return config, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode config: multiple JSON documents")
		}
		return fmt.Errorf("decode trailing config data: %w", err)
	}
	return nil
}

// Validate rejects ambiguous, unsafe or currently unsupported scenarios
// before a runner starts sending traffic.
func (c Config) Validate() error {
	if err := validateTarget(c.Target); err != nil {
		return err
	}
	if c.Rate.RequestsPerMinute < 1 || c.Rate.RequestsPerMinute > MaxRequestsPerMinute {
		return fmt.Errorf("rate.requestsPerMinute must be between 1 and %d", MaxRequestsPerMinute)
	}
	if c.Rate.Limiter != nil {
		switch c.Rate.Limiter.Type {
		case "", "local", "redis":
		default:
			return fmt.Errorf("rate.limiter.type %q is unsupported; use local or redis", c.Rate.Limiter.Type)
		}
	}
	if c.Rate.Profile != nil {
		switch c.Rate.Profile.Type {
		case "", "fixed", "uniform":
		default:
			return fmt.Errorf("rate.profile.type %q is unsupported; use fixed or uniform", c.Rate.Profile.Type)
		}
	}
	if len(c.Stores) > MaxStores {
		return fmt.Errorf("stores must contain at most %d entries", MaxStores)
	}

	stores := make(map[string]struct{}, len(c.Stores))
	for i, store := range c.Stores {
		if err := validateName(store.Name); err != nil {
			return fmt.Errorf("stores[%d].name: %w", i, err)
		}
		if _, exists := stores[store.Name]; exists {
			return fmt.Errorf("stores[%d].name %q is duplicated", i, store.Name)
		}
		if store.Capacity < 1 || store.Capacity > MaxStoreCapacity {
			return fmt.Errorf("stores[%d].capacity must be between 1 and %d", i, MaxStoreCapacity)
		}
		stores[store.Name] = struct{}{}
	}

	if len(c.Operations) == 0 || len(c.Operations) > MaxOperations {
		return fmt.Errorf("operations must contain between 1 and %d entries", MaxOperations)
	}
	operationNames := make(map[string]struct{}, len(c.Operations))
	for i, operation := range c.Operations {
		if err := validateOperation(operation, stores); err != nil {
			return fmt.Errorf("operations[%d]: %w", i, err)
		}
		if _, exists := operationNames[operation.Name]; exists {
			return fmt.Errorf("operations[%d].name %q is duplicated", i, operation.Name)
		}
		operationNames[operation.Name] = struct{}{}
	}
	return nil
}

func validateTarget(target TargetConfig) error {
	parsed, err := url.ParseRequestURI(target.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("target.baseURL must be an absolute HTTP(S) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("target.baseURL scheme %q is unsupported", parsed.Scheme)
	}
	if parsed.User != nil {
		return fmt.Errorf("target.baseURL must not contain credentials")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("target.baseURL must not contain a query or fragment")
	}
	return nil
}

func validateOperation(operation OperationConfig, stores map[string]struct{}) error {
	if err := validateName(operation.Name); err != nil {
		return fmt.Errorf("name: %w", err)
	}
	if operation.Weight < 1 || operation.Weight > 10_000 {
		return fmt.Errorf("weight must be between 1 and 10000")
	}
	if operation.Request.Method != http.MethodGet && operation.Request.Method != http.MethodPost {
		return fmt.Errorf("request.method %q is unsupported; use GET or POST", operation.Request.Method)
	}
	if !strings.HasPrefix(operation.Request.PathTemplate, "/") || strings.HasPrefix(operation.Request.PathTemplate, "//") {
		return fmt.Errorf("request.pathTemplate must begin with exactly one slash")
	}
	if strings.ContainsAny(operation.Request.PathTemplate, "\r\n\\#") {
		return fmt.Errorf("request.pathTemplate contains an unsupported character")
	}
	if len(operation.Request.BodyTemplate) > MaxRequestBodyBytes {
		return fmt.Errorf("request.bodyTemplate exceeds %d bytes", MaxRequestBodyBytes)
	}
	if operation.Request.Method == http.MethodGet && operation.Request.BodyTemplate != "" {
		return fmt.Errorf("GET request must not define bodyTemplate")
	}
	if err := validateHeaders(operation.Request.Headers); err != nil {
		return err
	}
	if len(operation.ExpectedStatusCodes) == 0 {
		return fmt.Errorf("expectedStatusCodes must not be empty")
	}
	seenStatus := map[int]struct{}{}
	for _, status := range operation.ExpectedStatusCodes {
		if status < 100 || status > 599 {
			return fmt.Errorf("expectedStatusCodes contains invalid HTTP status %d", status)
		}
		if _, exists := seenStatus[status]; exists {
			return fmt.Errorf("expectedStatusCodes contains duplicate HTTP status %d", status)
		}
		seenStatus[status] = struct{}{}
	}

	variables := make(map[string]struct{}, len(operation.Request.Variables))
	for i, variable := range operation.Request.Variables {
		if err := validateVariable(variable, stores); err != nil {
			return fmt.Errorf("request.variables[%d]: %w", i, err)
		}
		if _, exists := variables[variable.Name]; exists {
			return fmt.Errorf("request.variables[%d].name %q is duplicated", i, variable.Name)
		}
		variables[variable.Name] = struct{}{}
	}

	used, err := templateVariables(operation.Request.PathTemplate + "\n" + operation.Request.BodyTemplate)
	if err != nil {
		return err
	}
	for name := range used {
		if _, exists := variables[name]; !exists {
			return fmt.Errorf("template references undefined variable %q", name)
		}
	}
	for name := range variables {
		if _, exists := used[name]; !exists {
			return fmt.Errorf("variable %q is defined but unused", name)
		}
	}

	if operation.Capture != nil {
		if err := validateJSONPointer(operation.Capture.JSONPointer); err != nil {
			return fmt.Errorf("capture.jsonPointer: %w", err)
		}
		if _, exists := stores[operation.Capture.Store]; !exists {
			return fmt.Errorf("capture.store %q is not declared", operation.Capture.Store)
		}
	}
	return nil
}

func validateHeaders(headers map[string]string) error {
	for name, value := range headers {
		if name == "" || strings.ContainsAny(name, " \t\r\n:") {
			return fmt.Errorf("request.headers contains invalid header name %q", name)
		}
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("request.headers[%q] contains a line break", name)
		}
		if strings.Contains(value, "{{") || strings.Contains(value, "}}") {
			return fmt.Errorf("request.headers[%q] templates are unsupported in Phase 2", name)
		}
		switch strings.ToLower(name) {
		case "host", "content-length", "transfer-encoding", "connection":
			return fmt.Errorf("request.headers must not set transport header %q", name)
		}
	}
	return nil
}

func validateVariable(variable VariableConfig, stores map[string]struct{}) error {
	if err := validateName(variable.Name); err != nil {
		return fmt.Errorf("name: %w", err)
	}
	switch variable.Source.Type {
	case "randomUUID":
		if variable.Source.Store != "" || variable.Source.Length != 0 {
			return fmt.Errorf("randomUUID source must not set store or length")
		}
	case "randomBase62":
		if variable.Source.Store != "" || variable.Source.Length < 1 || variable.Source.Length > 128 {
			return fmt.Errorf("randomBase62 source requires length between 1 and 128 and no store")
		}
	case "store":
		if variable.Source.Length != 0 {
			return fmt.Errorf("store source must not set length")
		}
		if _, exists := stores[variable.Source.Store]; !exists {
			return fmt.Errorf("store source references undeclared store %q", variable.Source.Store)
		}
	default:
		return fmt.Errorf("source.type %q is unsupported", variable.Source.Type)
	}
	return nil
}

func validateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("%q must match %s", name, namePattern.String())
	}
	return nil
}

func templateVariables(template string) (map[string]struct{}, error) {
	variables := map[string]struct{}{}
	remaining := templateVarPattern.ReplaceAllStringFunc(template, func(match string) string {
		name := strings.TrimSpace(match[2 : len(match)-2])
		variables[name] = struct{}{}
		return ""
	})
	if strings.Contains(remaining, "{{") || strings.Contains(remaining, "}}") {
		return nil, fmt.Errorf("template contains malformed variable marker")
	}
	for name := range variables {
		if !namePattern.MatchString(name) {
			return nil, fmt.Errorf("template variable %q must match %s", name, namePattern.String())
		}
	}
	return variables, nil
}

func validateJSONPointer(pointer string) error {
	if !strings.HasPrefix(pointer, "/") {
		return fmt.Errorf("must begin with slash")
	}
	for i := 0; i < len(pointer); i++ {
		if pointer[i] != '~' {
			continue
		}
		if i+1 >= len(pointer) || (pointer[i+1] != '0' && pointer[i+1] != '1') {
			return fmt.Errorf("contains invalid RFC 6901 escape")
		}
		i++
	}
	return nil
}
