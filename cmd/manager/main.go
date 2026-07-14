// Command manager is the future entrypoint for the Kurama controller.
//
// Phase 0 intentionally has no Kubernetes client wiring yet: the API and
// runner contract are defined and tested before the controller begins to
// create cluster resources.
package main

import "log/slog"

func main() {
	slog.Info("Kurama manager starting", "phase", "foundation")
}
