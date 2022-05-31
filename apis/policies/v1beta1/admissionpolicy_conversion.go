package v1beta1

import (
	"github.com/kubewarden/kubewarden-controller/apis/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts this AdmissionPolicy to the Hub version (v1beta1).
func (src *AdmissionPolicy) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1alpha2.AdmissionPolicy)

	dst.Spec.PolicyServer = src.Spec.PolicyServer
	dst.Spec.Mode = v1alpha2.PolicyMode(PolicyMode(src.Spec.Mode))
	dst.Spec.Rules = src.Spec.Rules
	dst.Spec.Settings = src.Spec.Settings
	dst.Spec.Module = src.Spec.Module
	//TODO add all missing fields

	println(dst)

	return nil
}

// ConvertFrom converts from the Hub version (v1) to this version.
func (dst *AdmissionPolicy) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1alpha2.AdmissionPolicy)

	dst.Spec.PolicyServer = src.Spec.PolicyServer
	dst.Spec.Mode = PolicyMode(src.Spec.Mode)
	dst.Spec.Rules = src.Spec.Rules
	dst.Spec.Settings = src.Spec.Settings
	dst.Spec.Module = src.Spec.Module
	//TODO add all missing fields

	return nil
}
