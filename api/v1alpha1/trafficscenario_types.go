package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type TargetSpec struct {
	// BaseURL must be an absolute HTTP or HTTPS URL. Cluster-local Services are
	// expected to use their normal Kubernetes DNS name here.
	BaseURL string `json:"baseURL"`
}

type RateSpec struct {
	Schedule RateScheduleSpec `json:"schedule"`
	// Limiter selects how request permits are coordinated. When omitted, the
	// controller preserves the existing behaviour: memory storage uses a local
	// limiter and Redis storage uses a distributed Redis limiter.
	// +optional
	Limiter *RateLimiterSpec `json:"limiter,omitempty"`
	// Profile controls the delay between request attempts. When omitted, the
	// original fixed-interval scheduling behaviour is preserved.
	// +optional
	Profile *RateProfileSpec `json:"profile,omitempty"`
}

type RateScheduleType string

const (
	RateScheduleTypeFixed   RateScheduleType = "fixed"
	RateScheduleTypeUniform RateScheduleType = "uniform"
)

type RateScheduleSpec struct {
	// +kubebuilder:validation:Enum=fixed;uniform
	Type RateScheduleType `json:"type"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=6000
	// +optional
	RequestsPerMinute int `json:"requestsPerMinute,omitempty"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=6000
	// +optional
	MinRequestsPerMinute int `json:"minRequestsPerMinute,omitempty"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=6000
	// +optional
	MaxRequestsPerMinute int `json:"maxRequestsPerMinute,omitempty"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1440
	// +optional
	WindowMinutes int `json:"windowMinutes,omitempty"`
}

type RateLimiterType string

const (
	RateLimiterTypeLocal RateLimiterType = "local"
	RateLimiterTypeRedis RateLimiterType = "redis"
)

type RateLimiterSpec struct {
	// +kubebuilder:validation:Enum=local;redis
	// +optional
	Type RateLimiterType `json:"type,omitempty"`
}

type RateProfileType string

const (
	RateProfileTypeFixed   RateProfileType = "fixed"
	RateProfileTypeUniform RateProfileType = "uniform"
	RateProfileTypeBurst   RateProfileType = "burst"
)

type RateProfileSpec struct {
	// +kubebuilder:validation:Enum=fixed;uniform;burst
	// +optional
	Type RateProfileType `json:"type,omitempty"`
	// MinBurstSize is required for the burst profile and includes the first
	// request in each burst.
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=100
	// +optional
	MinBurstSize int `json:"minBurstSize,omitempty"`
	// MaxBurstSize is required for the burst profile and is inclusive.
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=100
	// +optional
	MaxBurstSize int `json:"maxBurstSize,omitempty"`
	// DelayDivisor controls how much faster requests inside a burst are sent
	// compared with the mean interval. The post-burst pause compensates for
	// this acceleration so the configured average rate is preserved. When
	// omitted from a burst profile, the runner uses 10.
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=100
	// +optional
	DelayDivisor int `json:"delayDivisor,omitempty"`
}

type StoreSpec struct {
	Name     string `json:"name"`
	Capacity int    `json:"capacity"`
}

type StorageType string

const (
	StorageTypeMemory StorageType = "memory"
	StorageTypeRedis  StorageType = "redis"
)

type StorageSpec struct {
	// Type selects the backend shared by all declared stores. An omitted value
	// preserves the in-memory backend used by existing scenarios.
	// +kubebuilder:validation:Enum=memory;redis
	// +optional
	Type StorageType `json:"type,omitempty"`
}

type VariableSourceSpec struct {
	Type   string `json:"type"`
	Store  string `json:"store,omitempty"`
	Length int    `json:"length,omitempty"`
}

type VariableSpec struct {
	Name   string             `json:"name"`
	Source VariableSourceSpec `json:"source"`
}

type RequestSpec struct {
	Method       string            `json:"method"`
	PathTemplate string            `json:"pathTemplate"`
	Headers      map[string]string `json:"headers,omitempty"`
	BodyTemplate string            `json:"bodyTemplate,omitempty"`
	Variables    []VariableSpec    `json:"variables,omitempty"`
}

type CaptureSpec struct {
	JSONPointer string `json:"jsonPointer"`
	Store       string `json:"store"`
}

type OperationSpec struct {
	Name                string       `json:"name"`
	Weight              int          `json:"weight"`
	Request             RequestSpec  `json:"request"`
	ExpectedStatusCodes []int        `json:"expectedStatusCodes"`
	Capture             *CaptureSpec `json:"capture,omitempty"`
}

// TrafficScenarioSpec is the desired HTTP workload and runner lifecycle.
// Suspending a scenario removes its runner Deployment without deleting its
// configuration.
type TrafficScenarioSpec struct {
	Target     TargetSpec      `json:"target"`
	Rate       RateSpec        `json:"rate"`
	Stores     []StoreSpec     `json:"stores,omitempty"`
	Operations []OperationSpec `json:"operations"`
	// +optional
	Storage *StorageSpec `json:"storage,omitempty"`
	// +optional
	Suspend bool `json:"suspend,omitempty"`
	// Replicas controls the number of runner Pods. Values greater than one
	// require a distributed Redis rate limiter.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +optional
	Replicas int32 `json:"replicas,omitempty"`
}

type TrafficScenarioPhase string

const (
	PhaseReady     TrafficScenarioPhase = "Ready"
	PhaseSuspended TrafficScenarioPhase = "Suspended"
	PhaseFailed    TrafficScenarioPhase = "Failed"
)

// TrafficScenarioStatus is controller-owned observed state.
type TrafficScenarioStatus struct {
	// +optional
	Phase TrafficScenarioPhase `json:"phase,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.target.baseURL`

// TrafficScenario declares a repeatable traffic generator. The controller
// turns it into a runner Deployment; it never sends target requests itself.
type TrafficScenario struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TrafficScenarioSpec   `json:"spec,omitempty"`
	Status TrafficScenarioStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type TrafficScenarioList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TrafficScenario `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TrafficScenario{}, &TrafficScenarioList{})
}
