// Code generated manually for the small v1alpha1 API surface. DO NOT EDIT.
package v1alpha1

import runtime "k8s.io/apimachinery/pkg/runtime"

func (in *CaptureSpec) DeepCopyInto(out *CaptureSpec) { *out = *in }
func (in *CaptureSpec) DeepCopy() *CaptureSpec {
	if in == nil {
		return nil
	}
	out := new(CaptureSpec)
	in.DeepCopyInto(out)
	return out
}
func (in *OperationSpec) DeepCopyInto(out *OperationSpec) {
	*out = *in
	in.Request.DeepCopyInto(&out.Request)
	if in.ExpectedStatusCodes != nil {
		out.ExpectedStatusCodes = append([]int(nil), in.ExpectedStatusCodes...)
	}
	if in.Capture != nil {
		out.Capture = in.Capture.DeepCopy()
	}
}
func (in *OperationSpec) DeepCopy() *OperationSpec {
	if in == nil {
		return nil
	}
	out := new(OperationSpec)
	in.DeepCopyInto(out)
	return out
}
func (in *RateSpec) DeepCopyInto(out *RateSpec) { *out = *in }
func (in *RateSpec) DeepCopy() *RateSpec {
	if in == nil {
		return nil
	}
	out := new(RateSpec)
	in.DeepCopyInto(out)
	return out
}
func (in *RequestSpec) DeepCopyInto(out *RequestSpec) {
	*out = *in
	if in.Headers != nil {
		out.Headers = make(map[string]string, len(in.Headers))
		for key, value := range in.Headers {
			out.Headers[key] = value
		}
	}
	if in.Variables != nil {
		out.Variables = append([]VariableSpec(nil), in.Variables...)
	}
}
func (in *RequestSpec) DeepCopy() *RequestSpec {
	if in == nil {
		return nil
	}
	out := new(RequestSpec)
	in.DeepCopyInto(out)
	return out
}
func (in *StoreSpec) DeepCopyInto(out *StoreSpec) { *out = *in }
func (in *StoreSpec) DeepCopy() *StoreSpec {
	if in == nil {
		return nil
	}
	out := new(StoreSpec)
	in.DeepCopyInto(out)
	return out
}
func (in *TargetSpec) DeepCopyInto(out *TargetSpec) { *out = *in }
func (in *TargetSpec) DeepCopy() *TargetSpec {
	if in == nil {
		return nil
	}
	out := new(TargetSpec)
	in.DeepCopyInto(out)
	return out
}
func (in *TrafficScenarioSpec) DeepCopyInto(out *TrafficScenarioSpec) {
	*out = *in
	if in.Stores != nil {
		out.Stores = append([]StoreSpec(nil), in.Stores...)
	}
	if in.Operations != nil {
		out.Operations = make([]OperationSpec, len(in.Operations))
		for i := range in.Operations {
			in.Operations[i].DeepCopyInto(&out.Operations[i])
		}
	}
}
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
func (in *VariableSourceSpec) DeepCopyInto(out *VariableSourceSpec) { *out = *in }
func (in *VariableSourceSpec) DeepCopy() *VariableSourceSpec {
	if in == nil {
		return nil
	}
	out := new(VariableSourceSpec)
	in.DeepCopyInto(out)
	return out
}
func (in *VariableSpec) DeepCopyInto(out *VariableSpec) { *out = *in }
func (in *VariableSpec) DeepCopy() *VariableSpec {
	if in == nil {
		return nil
	}
	out := new(VariableSpec)
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
