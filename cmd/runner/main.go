// Command runner is the workload Pod managed by a TrafficScenario.
// Phase 1 keeps it deliberately inert: it proves the controller-to-runner
// lifecycle before Phase 2 introduces HTTP traffic generation.
package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	slog.Info("Kurama runner ready", "config", "/etc/kurama/scenario.json")

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	slog.Info("Kurama runner stopping")
}
