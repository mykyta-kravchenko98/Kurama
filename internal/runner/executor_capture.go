package runner

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const MaxCapturedValueBytes = 4 << 10

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
