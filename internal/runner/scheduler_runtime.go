package runner

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

func logExecution(result ExecutionResult, err error) {
	attributes := []any{
		"operation", result.Operation,
		"status", result.StatusCode,
		"duration", result.Duration,
		"responseBytes", result.ResponseBytes,
		"captured", result.Captured,
	}
	if err == nil {
		slog.Info("Kurama request completed", attributes...)
		return
	}
	attributes = append(attributes, "error", err)
	if errors.Is(err, ErrStoreValueUnavailable) {
		slog.Warn("Kurama operation temporarily unavailable", attributes...)
		return
	}
	slog.Error("Kurama request failed", attributes...)
}

type waitForDelay func(ctx context.Context, delay time.Duration) bool

func waitWithTimer(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
