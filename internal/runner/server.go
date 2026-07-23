package runner

// Runner HTTP server constants are shared with the controller-generated Pod
// probes.
const (
	MetricsPortName = "metrics"
	MetricsPort     = int32(8080)
	MetricsPath     = "/metrics"
	HealthPath      = "/healthz"
	ReadinessPath   = "/readyz"
)
