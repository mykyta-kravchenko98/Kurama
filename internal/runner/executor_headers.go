package runner

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

func applyRequestHeaders(request *http.Request, config RequestConfig) error {
	for name, value := range config.Headers {
		request.Header.Set(name, value)
	}
	for i, header := range config.SecretHeaders {
		value, err := readSecretHeaderValue(header.ValueFile)
		if err != nil {
			// Never include the value in this error. File paths are controller-
			// generated and do not reveal the Secret name or key.
			return fmt.Errorf("read secret header %q at index %d: %w", header.Name, i, err)
		}
		request.Header.Set(header.Name, value)
	}
	return nil
}

func readSecretHeaderValue(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read projected value: %w", err)
	}
	if len(data) > MaxSecretHeaderBytes {
		return "", fmt.Errorf("projected value exceeds %d bytes", MaxSecretHeaderBytes)
	}
	value := strings.TrimRight(string(data), "\r\n")
	if value == "" {
		return "", fmt.Errorf("projected value is empty")
	}
	if strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("projected value contains a line break")
	}
	return value, nil
}

// SecretHeaderFiles returns the controller-generated credential file paths
// used by this runner configuration. It intentionally exposes paths only.
func (c Config) SecretHeaderFiles() []string {
	files := make([]string, 0)
	for _, operation := range c.Operations {
		for _, header := range operation.Request.SecretHeaders {
			files = append(files, header.ValueFile)
		}
	}
	return files
}

// CheckSecretHeaderFiles verifies that every projected credential is readable
// and valid without retaining or returning its value.
func CheckSecretHeaderFiles(paths []string) error {
	for i, path := range paths {
		if _, err := readSecretHeaderValue(path); err != nil {
			return fmt.Errorf("secret header file %d is not ready: %w", i, err)
		}
	}
	return nil
}
