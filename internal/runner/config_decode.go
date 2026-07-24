package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

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
