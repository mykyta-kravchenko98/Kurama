package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TargetSpec struct {
	// BaseURL must be an absolute HTTP or HTTPS URL. Cluster-local Services are
	// expected to use their normal Kubernetes DNS name here.
	// +kubebuilder:validation:Pattern=`^https?://.+`
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
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9-]{0,62}$`
	Name string `json:"name"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100000
	Capacity int `json:"capacity"`
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

// RunnerSpec configures resources for the generated runner workload. Security
// hardening is controller-owned and cannot be weakened through TrafficScenario.
type RunnerSpec struct {
	// Resources overrides the controller defaults. Omitted CPU, memory and
	// ephemeral-storage entries retain their default request or limit.
	// +optional
	Resources *RunnerResourcesSpec `json:"resources,omitempty"`
}

// RunnerResourcesSpec configures compute resources for the runner container.
type RunnerResourcesSpec struct {
	// +optional
	Requests corev1.ResourceList `json:"requests,omitempty"`
	// +optional
	Limits corev1.ResourceList `json:"limits,omitempty"`
}

type VariableSourceSpec struct {
	// +kubebuilder:validation:Enum=randomUUID;randomBase62;store
	Type string `json:"type"`
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9-]{0,62}$`
	// +optional
	Store string `json:"store,omitempty"`
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=128
	// +optional
	Length int `json:"length,omitempty"`
}

type VariableSpec struct {
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9-]{0,62}$`
	Name   string             `json:"name"`
	Source VariableSourceSpec `json:"source"`
}

// SecretKeySelector identifies one key in a Kubernetes Secret in the same
// namespace as the TrafficScenario. The secret value is mounted by kubelet;
// it is never copied into the runner ConfigMap.
type SecretKeySelector struct {
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
	// +kubebuilder:validation:Pattern=`^[-._a-zA-Z0-9]+$`
	// +kubebuilder:validation:MaxLength=253
	Key string `json:"key"`
}

type SecretHeaderValueSource struct {
	SecretKeyRef SecretKeySelector `json:"secretKeyRef"`
}

// SecretHeaderSpec sets an HTTP header from a Kubernetes Secret. Use this for
// authorization tokens and other credentials instead of literal headers.
type SecretHeaderSpec struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Name      string                  `json:"name"`
	ValueFrom SecretHeaderValueSource `json:"valueFrom"`
}

type RequestSpec struct {
	// +kubebuilder:validation:Enum=GET;POST
	Method string `json:"method"`
	// +kubebuilder:validation:MinLength=1
	PathTemplate string `json:"pathTemplate"`
	// Known credential-bearing headers cannot be set literally; use
	// SecretHeaders for those values.
	// +kubebuilder:validation:MaxProperties=64
	// +kubebuilder:validation:XValidation:rule="self.all(key, !key.lowerAscii().matches('^(authorization|proxy-authorization|cookie|set-cookie|x-api-key)$'))",message="literal sensitive headers are forbidden; use secretHeaders"
	// +optional
	Headers map[string]string `json:"headers,omitempty"`
	// SecretHeaders reads header values from Kubernetes Secrets in the same
	// namespace. Secret values are projected into the runner Pod and are not
	// stored in the TrafficScenario or generated ConfigMap.
	// +kubebuilder:validation:MaxItems=64
	// +optional
	SecretHeaders []SecretHeaderSpec `json:"secretHeaders,omitempty"`
	// +kubebuilder:validation:MaxLength=65536
	// +optional
	BodyTemplate string `json:"bodyTemplate,omitempty"`
	// +optional
	Variables []VariableSpec `json:"variables,omitempty"`
}

type CaptureSpec struct {
	// +kubebuilder:validation:Pattern=`^/`
	JSONPointer string `json:"jsonPointer"`
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9-]{0,62}$`
	Store string `json:"store"`
}

type OperationSpec struct {
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9-]{0,62}$`
	Name string `json:"name"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10000
	Weight  int         `json:"weight"`
	Request RequestSpec `json:"request"`
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:Minimum=100
	// +kubebuilder:validation:items:Maximum=599
	ExpectedStatusCodes []int `json:"expectedStatusCodes"`
	// +optional
	Capture *CaptureSpec `json:"capture,omitempty"`
}

// TrafficScenarioSpec is the desired HTTP workload and runner lifecycle.
// Suspending a scenario removes its runner Deployment without deleting its
// configuration.
type TrafficScenarioSpec struct {
	Target TargetSpec `json:"target"`
	Rate   RateSpec   `json:"rate"`
	// +kubebuilder:validation:MaxItems=32
	// +optional
	Stores []StoreSpec `json:"stores,omitempty"`
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
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
	// Runner configures the generated runner workload.
	// +optional
	Runner *RunnerSpec `json:"runner,omitempty"`
}

type TrafficScenarioPhase string

const (
	PhaseProgressing TrafficScenarioPhase = "Progressing"
	PhaseReady       TrafficScenarioPhase = "Ready"
	PhaseDegraded    TrafficScenarioPhase = "Degraded"
	PhaseSuspended   TrafficScenarioPhase = "Suspended"
	PhaseFailed      TrafficScenarioPhase = "Failed"
)

const (
	ConditionReady       = "Ready"
	ConditionProgressing = "Progressing"
	ConditionDegraded    = "Degraded"
	ConditionSuspended   = "Suspended"
)

// TrafficScenarioStatus is controller-owned observed state.
type TrafficScenarioStatus struct {
	// +optional
	Phase TrafficScenarioPhase `json:"phase,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions describe the observed runner lifecycle and rollout state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ts
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
