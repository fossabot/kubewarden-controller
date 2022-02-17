package policies

import (
	"context"
	"fmt"

	"github.com/kubewarden/kubewarden-controller/apis/policies/v1alpha2"
	policiesv1alpha2 "github.com/kubewarden/kubewarden-controller/apis/policies/v1alpha2"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/constants"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/naming"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func gcPolicy(ctx context.Context, r client.Client, admissionPolicy v1alpha2.Policy) (ctrl.Result, error) {
	policyServerWasDeleted := false
	if admissionPolicy.GetPolicyServer() != "" {
		policyServer := policiesv1alpha2.PolicyServer{}
		if err := r.Get(ctx, types.NamespacedName{Name: admissionPolicy.GetPolicyServer()}, &policyServer); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, errors.Wrapf(err, "could not get policy server %s", admissionPolicy.GetPolicyServer())
			}
			// If we cannot find the policy server, but the policy was
			// pending or active at some point in time, we can ensure that
			// the policy server had to be deleted, so we can GC this policy
			policyServerWasDeleted =
				admissionPolicy.GetStatus().PolicyStatus == policiesv1alpha2.PolicyStatusPending ||
					admissionPolicy.GetStatus().PolicyStatus == policiesv1alpha2.PolicyStatusActive
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
	if err := r.Update(ctx, admissionPolicy); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update admission policy: %w", err)
	}
	return ctrl.Result{}, nil
}

func setPolicyStatus(ctx context.Context, deploymentsNamespace string, client client.Client, apiReader client.Reader, policy v1alpha2.Policy) (ctrl.Result, error) {
	if policy.GetStatus().PolicyStatus == "" {
		// If the policy status is empty, default to "unscheduled" if a
		// policy server is not assigned. Set to "scheduled" if there is.
		if policy.GetPolicyServer() == "" {
			policy.SetStatus(policiesv1alpha2.PolicyStatusUnscheduled)
		} else {
			policy.SetStatus(policiesv1alpha2.PolicyStatusScheduled)
		}
	} else if policy.GetStatus().PolicyStatus == policiesv1alpha2.PolicyStatusUnscheduled {
		// If the policy status is "unscheduled", and now we observe a
		// policy server is assigned, set to "scheduled".
		if policy.GetPolicyServer() != "" {
			policy.SetStatus(policiesv1alpha2.PolicyStatusScheduled)
		}
	} else if policy.GetStatus().PolicyStatus == policiesv1alpha2.PolicyStatusScheduled {
		// If the policy status is "scheduled", and now we observe a
		// policy server that exists, set to "pending".
		policyServer := policiesv1alpha2.PolicyServer{}
		if err := apiReader.Get(ctx, types.NamespacedName{Name: policy.GetPolicyServer()}, &policyServer); err == nil {
			policy.SetStatus(policiesv1alpha2.PolicyStatusPending)
		}
	}

	policyServerDeployment := appsv1.Deployment{}
	if err := apiReader.Get(ctx, types.NamespacedName{Namespace: deploymentsNamespace, Name: naming.PolicyServerDeploymentNameForPolicyServerName(policy.GetPolicyServer())}, &policyServerDeployment); err != nil {
		return ctrl.Result{Requeue: true}, nil
	}

	policyServerConfigMap := corev1.ConfigMap{}
	if err := apiReader.Get(ctx, types.NamespacedName{Namespace: deploymentsNamespace, Name: naming.PolicyServerDeploymentNameForPolicyServerName(policy.GetPolicyServer())}, &policyServerConfigMap); err != nil {
		return ctrl.Result{Requeue: true}, nil
	}

	policyMap, err := getPolicyMapFromConfigMap(&policyServerConfigMap)
	if err == nil {
		if policyConfig, ok := policyMap[policy.GetUniqueName()]; ok {
			policy.SetPolicyModeStatus(policiesv1alpha2.PolicyModeStatus(policyConfig.PolicyMode))
		} else {
			policy.SetPolicyModeStatus(policiesv1alpha2.PolicyModeStatusUnknown)
		}
	} else {
		policy.SetPolicyModeStatus(policiesv1alpha2.PolicyModeStatusUnknown)
	}

	policyStatus := policy.GetStatus()
	SetPolicyConfigurationCondition(&policyServerConfigMap, &policyServerDeployment, &policyStatus.Conditions)
	SetPolicyUniquenessCondition(ctx, apiReader, &policyServerConfigMap, &policyServerDeployment, &policyStatus.Conditions)

	// Update status
	err = client.Status().Update(ctx, policy)
	return ctrl.Result{}, errors.Wrap(err, "failed to update status")
}
