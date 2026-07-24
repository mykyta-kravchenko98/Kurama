package runner

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

var (
	namePattern        = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	templateVarPattern = regexp.MustCompile(`\{\{([^{}]*)\}\}`)
)

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
	if !strings.HasPrefix(operation.Request.PathTemplate, "/") ||
		strings.HasPrefix(operation.Request.PathTemplate, "//") {
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
