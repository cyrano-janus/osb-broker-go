package v1alpha1

import "k8s.io/apimachinery/pkg/runtime"

// DeepCopy-Methoden von Hand, nicht von controller-gen erzeugt.
//
// Das Repo hat keine Codegen-Kette und eine bewusst schlanke
// Abhaengigkeitsliste; fuer vier Typen ist ein Generator plus dessen
// Toolchain teurer als der Code hier. Der Preis: wer ein Feld ergaenzt, muss
// hier nachziehen - deshalb prueft ein Test, dass jede Kopie wirklich
// entkoppelt ist, statt sich auf Sorgfalt zu verlassen.

func (in *OSBContext) DeepCopyInto(out *OSBContext) { *out = *in }

func (in *AppliedObjectRef) DeepCopyInto(out *AppliedObjectRef) { *out = *in }

func (in *OSBServiceInstanceSpec) DeepCopyInto(out *OSBServiceInstanceSpec) {
	*out = *in
	in.Context.DeepCopyInto(&out.Context)
	if in.Parameters != nil {
		out.Parameters = in.Parameters.DeepCopy()
	}
	if in.AppliedObjects != nil {
		out.AppliedObjects = make([]string, len(in.AppliedObjects))
		copy(out.AppliedObjects, in.AppliedObjects)
	}
	if in.AppliedRefs != nil {
		out.AppliedRefs = make([]AppliedObjectRef, len(in.AppliedRefs))
		copy(out.AppliedRefs, in.AppliedRefs)
	}
}

func (in *OSBServiceInstance) DeepCopyInto(out *OSBServiceInstance) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
}

func (in *OSBServiceInstance) DeepCopy() *OSBServiceInstance {
	if in == nil {
		return nil
	}
	out := new(OSBServiceInstance)
	in.DeepCopyInto(out)
	return out
}

func (in *OSBServiceInstance) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *OSBServiceInstanceList) DeepCopyInto(out *OSBServiceInstanceList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]OSBServiceInstance, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *OSBServiceInstanceList) DeepCopy() *OSBServiceInstanceList {
	if in == nil {
		return nil
	}
	out := new(OSBServiceInstanceList)
	in.DeepCopyInto(out)
	return out
}

func (in *OSBServiceInstanceList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *OSBServiceBindingSpec) DeepCopyInto(out *OSBServiceBindingSpec) {
	*out = *in
	in.Context.DeepCopyInto(&out.Context)
	if in.Parameters != nil {
		out.Parameters = in.Parameters.DeepCopy()
	}
	if in.VolumeMounts != nil {
		out.VolumeMounts = in.VolumeMounts.DeepCopy()
	}
}

func (in *OSBServiceBinding) DeepCopyInto(out *OSBServiceBinding) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
}

func (in *OSBServiceBinding) DeepCopy() *OSBServiceBinding {
	if in == nil {
		return nil
	}
	out := new(OSBServiceBinding)
	in.DeepCopyInto(out)
	return out
}

func (in *OSBServiceBinding) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *OSBServiceBindingList) DeepCopyInto(out *OSBServiceBindingList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]OSBServiceBinding, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *OSBServiceBindingList) DeepCopy() *OSBServiceBindingList {
	if in == nil {
		return nil
	}
	out := new(OSBServiceBindingList)
	in.DeepCopyInto(out)
	return out
}

func (in *OSBServiceBindingList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
