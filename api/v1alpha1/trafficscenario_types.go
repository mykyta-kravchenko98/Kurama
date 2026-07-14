package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type TargetSpec struct {
	// BaseURL must be an absolute HTTP or HTTPS URL. Cluster-local Services are
	// expected to use their normal Kubernetes DNS name here.
	BaseURL string `json:"baseURL"`
}

// TrafficScenarioSpec is the desired runner lifecycle. Suspending a scenario
// removes its runner Deployment without deleting the scenario configuration.
type TrafficScenarioSpec struct {
	Target TargetSpec `json:"target"`
	// +optional
	Suspend bool `json:"suspend,omitempty"`
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
