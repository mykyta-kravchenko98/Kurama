package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var ErrStoreValueUnavailable = errors.New("store value unavailable")

// ValueGenerator produces template values. It is injectable so executor tests
// are deterministic without weakening production randomness.
type ValueGenerator interface {
	UUID() (string, error)
	Base62(length int) (string, error)
}

func (e *Executor) resolveVariables(ctx context.Context, variables []VariableConfig) (map[string]string, error) {
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
			value, ok, err = e.stores.Random(ctx, variable.Source.Store)
			if err != nil {
				return nil, fmt.Errorf("read store %q: %w", variable.Source.Store, err)
			}
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
