// +kubebuilder:object:generate=true
// +groupName=traffic.kurama.dev
//
//go:generate go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0 object paths=.
//go:generate go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0 crd paths=. output:crd:artifacts:config=../../config/crd/bases
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	GroupVersion  = schema.GroupVersion{Group: "traffic.kurama.dev", Version: "v1alpha1"}
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}
	AddToScheme   = SchemeBuilder.AddToScheme
)
