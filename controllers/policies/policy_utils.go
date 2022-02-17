package policies

import (
	"context"
	"fmt"

	"github.com/kubewarden/kubewarden-controller/apis/policies/v1alpha2"
	policiesv1alpha2 "github.com/kubewarden/kubewarden-controller/apis/policies/v1alpha2"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/constants"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func getPolicy(ctx context.Context, client client.Reader, namespace, name string) (policiesv1alpha2.Policy, error) {
	if namespace == "" {
		clusterAdmissionPolicy := policiesv1alpha2.ClusterAdmissionPolicy{}
		err := client.Get(ctx, types.NamespacedName{Name: name}, &clusterAdmissionPolicy)
		return &clusterAdmissionPolicy, err
	}
	admissionPolicy := policiesv1alpha2.AdmissionPolicy{}
	err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &admissionPolicy)
	return &admissionPolicy, err
}

func gcPolicy(ctx context.Context, r client.Client, admissionPolicy v1alpha2.Policy) (ctrl.Result, error) {
	policyServerWasDeleted := false
	if admissionPolicy.GetPolicyServer() != "" {
		policyServer := policiesv1alpha2.PolicyServer{}
		if err := r.Get(ctx, types.NamespacedName{Name: admissionPolicy.GetPolicyServer()}, &policyServer); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, errors.Wrapf(err, "could not get policy server %s", admissionPolicy.GetPolicyServer())
			}
			// If we cannot find the policy server, but the policy was
			// active at some point in time, we can ensure that the policy
			// server had to be deleted, so we can GC this policy
			policyServerWasDeleted = admissionPolicy.GetStatus().PolicyStatus == policiesv1alpha2.PolicyStatusActive
		} else {
			policyServerWasDeleted = policyServer.DeletionTimestamp != nil
		}
	}
	if admissionPolicy.GetObjectMeta().DeletionTimestamp == nil {
		if policyServerWasDeleted {
			if err := r.Delete(ctx, admissionPolicy); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		} else {
			return ctrl.Result{}, nil
		}
	}
	controllerutil.RemoveFinalizer(admissionPolicy, constants.KubewardenFinalizer)
	if err := r.Update(context.TODO(), admissionPolicy); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update admission policy: %w", err)
	}
	return ctrl.Result{}, nil
}
