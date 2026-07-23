package main

import (
	"fmt"
	"os"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
)

func metricsAddressFromEnv() string {
	if address := os.Getenv(runner.MetricsAddrEnv); address != "" {
		return address
	}
	return defaultMetricsAddress
}

func loadConfig(path string) (runner.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return runner.Config{}, fmt.Errorf("open runner config %q: %w", path, err)
	}
	config, decodeErr := runner.DecodeConfig(file)
	closeErr := file.Close()
	if decodeErr != nil {
		return runner.Config{}, fmt.Errorf("load runner config %q: %w", path, decodeErr)
	}
	if closeErr != nil {
		return runner.Config{}, fmt.Errorf("close runner config %q: %w", path, closeErr)
	}
	return config, nil
}
