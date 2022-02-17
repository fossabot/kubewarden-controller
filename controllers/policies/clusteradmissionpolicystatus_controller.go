/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package policies

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	policiesv1alpha2 "github.com/kubewarden/kubewarden-controller/apis/policies/v1alpha2"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/admission"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/constants"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/naming"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ClusterAdmissionPolicyStatusReconciler reconciles a ClusterAdmissionPolicy status object
type ClusterAdmissionPolicyStatusReconciler struct {
	client.Client
	Log        logr.Logger
	Scheme     *runtime.Scheme
	Reconciler admission.Reconciler
}

// Reconcile takes care of reconciling ClusterAdmissionPolicy status subresources
func (r *ClusterAdmissionPolicyStatusReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	clusterAdmissionPolicy := policiesv1alpha2.ClusterAdmissionPolicy{}
	if err := r.Reconciler.APIReader.Get(ctx, types.NamespacedName{Name: req.Name}, &clusterAdmissionPolicy); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("cannot retrieve cluster admission policy: %w", err)
	}

	if clusterAdmissionPolicy.Status.PolicyStatus == "" {
		// If the policy status is empty, default to "unscheduled" if a
		// policy server is not assigned. Set to "scheduled" if there is.
		if clusterAdmissionPolicy.Spec.PolicyServer == "" {
			clusterAdmissionPolicy.SetStatus(policiesv1alpha2.PolicyStatusUnscheduled)
		} else {
			clusterAdmissionPolicy.SetStatus(policiesv1alpha2.PolicyStatusScheduled)
		}
	} else if clusterAdmissionPolicy.Status.PolicyStatus == policiesv1alpha2.PolicyStatusUnscheduled {
		// If the policy status is "unscheduled", and now we observe a
		// policy server is assigned, set to "scheduled".
		if clusterAdmissionPolicy.Spec.PolicyServer != "" {
			clusterAdmissionPolicy.SetStatus(policiesv1alpha2.PolicyStatusScheduled)
		}
	} else if clusterAdmissionPolicy.Status.PolicyStatus == policiesv1alpha2.PolicyStatusScheduled {
		// If the policy status is "schduled", and now we observe a
		// policy server that exists, set to "pending".
		policyServer := policiesv1alpha2.PolicyServer{}
		if err := r.Reconciler.APIReader.Get(ctx, types.NamespacedName{Name: clusterAdmissionPolicy.Spec.PolicyServer}, &policyServer); err == nil {
			clusterAdmissionPolicy.SetStatus(policiesv1alpha2.PolicyStatusPending)
		}
	}

	r.Status().Update(ctx, &clusterAdmissionPolicy)

	policyServerDeployment := appsv1.Deployment{}
	if err := r.Reconciler.APIReader.Get(ctx, types.NamespacedName{Namespace: r.Reconciler.DeploymentsNamespace, Name: naming.PolicyServerDeploymentNameForPolicyServerName(clusterAdmissionPolicy.Spec.PolicyServer)}, &policyServerDeployment); err != nil {
		return ctrl.Result{Requeue: true}, nil
	}

	policyServerConfigMap := corev1.ConfigMap{}
	if err := r.Reconciler.APIReader.Get(ctx, types.NamespacedName{Namespace: r.Reconciler.DeploymentsNamespace, Name: naming.PolicyServerDeploymentNameForPolicyServerName(clusterAdmissionPolicy.Spec.PolicyServer)}, &policyServerConfigMap); err != nil {
		return ctrl.Result{Requeue: true}, nil
	}

	policyMap, err := getPolicyMapFromConfigMap(&policyServerConfigMap)
	if err == nil {
		if policyConfig, ok := policyMap[clusterAdmissionPolicy.GetUniqueName()]; ok {
			clusterAdmissionPolicy.Status.PolicyMode = policiesv1alpha2.PolicyModeStatus(policyConfig.PolicyMode)
		} else {
			clusterAdmissionPolicy.Status.PolicyMode = policiesv1alpha2.PolicyModeStatusUnknown
		}
	} else {
		clusterAdmissionPolicy.Status.PolicyMode = policiesv1alpha2.PolicyModeStatusUnknown
	}

	SetPolicyConfigurationCondition(&policyServerConfigMap, &policyServerDeployment, &clusterAdmissionPolicy.Status.Conditions)
	SetPolicyUniquenessCondition(ctx, r.Reconciler.APIReader, &policyServerConfigMap, &policyServerDeployment, &clusterAdmissionPolicy.Status.Conditions)

	// Update status
	err = r.Status().Update(ctx, &clusterAdmissionPolicy)
	return ctrl.Result{Requeue: true}, errors.Wrap(err, "failed to update status")
}

// SetupWithManager sets up the controller with the Manager.
// nolint:wrapcheck
func (r *ClusterAdmissionPolicyStatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&policiesv1alpha2.ClusterAdmissionPolicy{}).
		Watches(
			&source.Kind{Type: &corev1.ConfigMap{}},
			handler.EnqueueRequestsFromMapFunc(r.findClusterAdmissionPoliciesForConfigMap),
		).
		Watches(
			&source.Kind{Type: &corev1.Pod{}},
			handler.EnqueueRequestsFromMapFunc(r.findClusterAdmissionPoliciesForPod),
		).
		Complete(r)
}

func (r *ClusterAdmissionPolicyStatusReconciler) findClusterAdmissionPoliciesForConfigMap(object client.Object) []reconcile.Request {
	configMap, ok := object.(*corev1.ConfigMap)
	if !ok {
		return []reconcile.Request{}
	}
	res := []reconcile.Request{}
	policyMap, err := getPolicyMapFromConfigMap(configMap)
	if err != nil {
		return res
	}
	return policyMap.ToClusterAdmissionPolicyReconcileRequests()
}

func (r *ClusterAdmissionPolicyStatusReconciler) findClusterAdmissionPoliciesForPod(object client.Object) []reconcile.Request {
	pod, ok := object.(*corev1.Pod)
	if !ok {
		return []reconcile.Request{}
	}
	policyServerName, ok := pod.Labels[constants.PolicyServerLabelKey]
	if !ok {
		return []reconcile.Request{}
	}
	policyServerDeploymentName := naming.PolicyServerDeploymentNameForPolicyServerName(policyServerName)
	configMap := corev1.ConfigMap{}
	err := r.Reconciler.APIReader.Get(context.TODO(), client.ObjectKey{
		Namespace: pod.ObjectMeta.Namespace,
		Name:      policyServerDeploymentName, // As the deployment name matches the name of the ConfigMap
	}, &configMap)
	if err != nil {
		return []reconcile.Request{}
	}
	return r.findClusterAdmissionPoliciesForConfigMap(&configMap)
}
