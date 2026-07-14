// Code generated manually for the small v1alpha1 API surface. DO NOT EDIT.
package v1alpha1

import runtime "k8s.io/apimachinery/pkg/runtime"

func (in *TargetSpec) DeepCopyInto(out *TargetSpec) { *out = *in }
func (in *TargetSpec) DeepCopy() *TargetSpec {
	if in == nil {
		return nil
	}
	out := new(TargetSpec)
	in.DeepCopyInto(out)
	return out
}
func (in *TrafficScenarioSpec) DeepCopyInto(out *TrafficScenarioSpec) { *out = *in }
func (in *TrafficScenarioSpec) DeepCopy() *TrafficScenarioSpec {
	if in == nil {
		return nil
	}
	out := new(TrafficScenarioSpec)
	in.DeepCopyInto(out)
	return out
}
func (in *TrafficScenarioStatus) DeepCopyInto(out *TrafficScenarioStatus) { *out = *in }
func (in *TrafficScenarioStatus) DeepCopy() *TrafficScenarioStatus {
	if in == nil {
		return nil
	}
	out := new(TrafficScenarioStatus)
	in.DeepCopyInto(out)
	return out
}
func (in *TrafficScenario) DeepCopyInto(out *TrafficScenario) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}
func (in *TrafficScenario) DeepCopy() *TrafficScenario {
	if in == nil {
		return nil
	}
	out := new(TrafficScenario)
	in.DeepCopyInto(out)
	return out
}
func (in *TrafficScenario) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
func (in *TrafficScenarioList) DeepCopyInto(out *TrafficScenarioList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]TrafficScenario, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
func (in *TrafficScenarioList) DeepCopy() *TrafficScenarioList {
	if in == nil {
		return nil
	}
	out := new(TrafficScenarioList)
	in.DeepCopyInto(out)
	return out
}
func (in *TrafficScenarioList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
