package runner

// Runner environment variables are shared by the controller that creates the
// Pod and the runner process that consumes them.
const (
	StoreBackendEnv = "KURAMA_STORE_BACKEND"
	RedisAddressEnv = "KURAMA_REDIS_ADDR"
	NamespaceEnv    = "KURAMA_NAMESPACE"
	ScenarioEnv     = "KURAMA_SCENARIO"
	MetricsAddrEnv  = "KURAMA_METRICS_ADDR"
)
