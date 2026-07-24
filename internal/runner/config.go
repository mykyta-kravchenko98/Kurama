package runner

const (
	// MaxConfigBytes stays within the practical size of a ConfigMap-backed
	// scenario and prevents an unexpectedly large local file from being read.
	MaxConfigBytes = 1 << 20
	// MaxRequestBodyBytes bounds a rendered request body. Phase 2 targets
	// ordinary JSON APIs rather than file uploads or multipart requests.
	MaxRequestBodyBytes = 64 << 10
	// MaxResponseBodyBytes bounds response data retained for validation and
	// capture. The executor will read one additional byte to detect overflow.
	MaxResponseBodyBytes = 1 << 20

	MaxRequestsPerMinute     = 6_000
	MaxScheduleWindowMinutes = 1_440
	MaxProfileBurstSize      = 100
	MaxProfileDelayDivisor   = 100
	MaxOperations            = 64
	MaxStores                = 32
	MaxStoreCapacity         = 100_000
)

// Config is the version-independent contract consumed by a runner Pod. It
// contains no Kubernetes types so execution can be tested with httptest and
// reused outside a controller process.
type Config struct {
	Target     TargetConfig      `json:"target"`
	Rate       RateConfig        `json:"rate"`
	Stores     []StoreConfig     `json:"stores,omitempty"`
	Operations []OperationConfig `json:"operations"`
}

type TargetConfig struct {
	BaseURL string `json:"baseURL"`
}

type RateConfig struct {
	Schedule RateScheduleConfig `json:"schedule"`
	Limiter  *RateLimiterConfig `json:"limiter,omitempty"`
	Profile  *RateProfileConfig `json:"profile,omitempty"`
}

type RateLimiterConfig struct {
	Type string `json:"type,omitempty"`
}

type RateProfileConfig struct {
	Type         string `json:"type,omitempty"`
	MinBurstSize int    `json:"minBurstSize,omitempty"`
	MaxBurstSize int    `json:"maxBurstSize,omitempty"`
	DelayDivisor int    `json:"delayDivisor,omitempty"`
}

type RateScheduleConfig struct {
	Type                 string `json:"type"`
	RequestsPerMinute    int    `json:"requestsPerMinute,omitempty"`
	MinRequestsPerMinute int    `json:"minRequestsPerMinute,omitempty"`
	MaxRequestsPerMinute int    `json:"maxRequestsPerMinute,omitempty"`
	WindowMinutes        int    `json:"windowMinutes,omitempty"`
}

type StoreConfig struct {
	Name     string `json:"name"`
	Capacity int    `json:"capacity"`
}

type OperationConfig struct {
	Name                string         `json:"name"`
	Weight              int            `json:"weight"`
	Request             RequestConfig  `json:"request"`
	ExpectedStatusCodes []int          `json:"expectedStatusCodes"`
	Capture             *CaptureConfig `json:"capture,omitempty"`
}

type RequestConfig struct {
	Method       string            `json:"method"`
	PathTemplate string            `json:"pathTemplate"`
	Headers      map[string]string `json:"headers,omitempty"`
	BodyTemplate string            `json:"bodyTemplate,omitempty"`
	Variables    []VariableConfig  `json:"variables,omitempty"`
}

type VariableConfig struct {
	Name   string         `json:"name"`
	Source VariableSource `json:"source"`
}

type VariableSource struct {
	// Type is one of randomUUID, randomBase62 or store.
	Type string `json:"type"`
	// Store is required only for type=store.
	Store string `json:"store,omitempty"`
	// Length is required only for type=randomBase62.
	Length int `json:"length,omitempty"`
}

type CaptureConfig struct {
	// JSONPointer is an RFC 6901 pointer into a JSON response, for example
	// /shortURL. Capturing the entire document is not supported in Phase 2.
	JSONPointer string `json:"jsonPointer"`
	Store       string `json:"store"`
}
