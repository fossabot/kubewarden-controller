/*


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
	"time"

	"github.com/go-logr/logr"
	policiesv1alpha2 "github.com/kubewarden/kubewarden-controller/apis/policies/v1alpha2"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/admission"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/constants"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

//+kubebuilder:rbac:groups=policies.kubewarden.io,resources=clusteradmissionpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policies.kubewarden.io,resources=clusteradmissionpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=policies.kubewarden.io,resources=clusteradmissionpolicies/finalizers,verbs=update

// PolicyServerReconciler reconciles a PolicyServer object
type PolicyServerReconciler struct {
	client.Client
	Log        logr.Logger
	Scheme     *runtime.Scheme
	Reconciler admission.Reconciler
}

// ClusterAdmissionPolicy RBAC
//+kubebuilder:rbac:groups=policies.kubewarden.io,resources=clusteradmissionpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policies.kubewarden.io,resources=clusteradmissionpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=policies.kubewarden.io,resources=clusteradmissionpolicies/finalizers,verbs=update

//+kubebuilder:rbac:groups=policies.kubewarden.io,resources=policyservers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policies.kubewarden.io,resources=policyservers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=policies.kubewarden.io,resources=policyservers/finalizers,verbs=update
//
// The following ought to be part of kubewarden-controller-manager-cluster-role:
//+kubebuilder:rbac:groups=core,resources=secrets;configmaps,verbs=list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=list;watch
//
// The following ought to be part of kubewarden-controller-manager-namespaced-role:
//+kubebuilder:rbac:groups=core,resources=secrets;services;configmaps,verbs=get;list;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;create;delete;update;patch

func (r *PolicyServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("policyserver", req.NamespacedName)

	admissionReconciler := r.Reconciler
	admissionReconciler.Log = log

	var policyServer policiesv1alpha2.PolicyServer
	if err := r.Get(ctx, req.NamespacedName, &policyServer); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("cannot retrieve policy server: %w", err)
	}

	if policyServer.ObjectMeta.DeletionTimestamp != nil {
		err := admissionReconciler.ReconcileDeletion(ctx, &policyServer)
		if err != nil {
			err = fmt.Errorf("failed reconciling deletion of policyServer %s: %w",
				policyServer.Name, err)
		}
		return ctrl.Result{}, err
	} else {
		if err := admissionReconciler.Reconcile(ctx, &policyServer); err != nil {
			if admission.IsPolicyServerNotReady(err) {
				log.Info("delaying policy registration since policy server is not yet ready")
				return ctrl.Result{
					Requeue:      true,
					RequeueAfter: time.Second * 5,
				}, nil
			}
			return ctrl.Result{}, fmt.Errorf("reconciliation error: %w", err)
		}
	}

	if err := r.Client.Status().Update(ctx, &policyServer); err != nil {
		return ctrl.Result{}, fmt.Errorf("update policy server status error: %w", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PolicyServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	err := mgr.GetFieldIndexer().IndexField(context.Background(), &policiesv1alpha2.ClusterAdmissionPolicy{}, constants.PolicyServerIndexKey, func(object client.Object) []string {
		policy, ok := object.(*policiesv1alpha2.ClusterAdmissionPolicy)
		if !ok {
			r.Log.Error(nil, "object is not type of ClusterAdmissionPolicy: %#v", policy)
			return []string{}
		}
		return []string{policy.Spec.PolicyServer}
	})
	if err != nil {
		return fmt.Errorf("failed enrolling controller with manager: %w", err)
	}
	err = mgr.GetFieldIndexer().IndexField(context.Background(), &policiesv1alpha2.AdmissionPolicy{}, constants.PolicyServerIndexKey, func(object client.Object) []string {
		policy, ok := object.(*policiesv1alpha2.AdmissionPolicy)
		if !ok {
			r.Log.Error(nil, "object is not type of ClusterAdmissionPolicy: %#v", policy)
			return []string{}
		}
		return []string{policy.Spec.PolicyServer}
	})
	if err != nil {
		return fmt.Errorf("failed enrolling controller with manager: %w", err)
	}
	err = ctrl.NewControllerManagedBy(mgr).
		For(&policiesv1alpha2.PolicyServer{}).
		Watches(&source.Kind{Type: &corev1.ConfigMap{}}, handler.EnqueueRequestsFromMapFunc(func(object client.Object) []reconcile.Request {
			configMap, ok := object.(*corev1.ConfigMap)
			if !ok {
				r.Log.Info("object is not type of ConfigMap: %+v", configMap)
				return []ctrl.Request{}
			}
			return []ctrl.Request{
				{
					NamespacedName: client.ObjectKey{
						Name: configMap.Labels[constants.PolicyServerLabelKey],
					},
				},
			}
		})).
		Watches(&source.Kind{Type: &policiesv1alpha2.AdmissionPolicy{}}, handler.EnqueueRequestsFromMapFunc(func(object client.Object) []reconcile.Request {
			// The watch will trigger twice per object change; once with the old
			// object, and once the new object. We need to be mindful when doing
			// Updates since they will invalidate the newever versions of the
			// object.
			policy, ok := object.(*policiesv1alpha2.AdmissionPolicy)
			if !ok {
				r.Log.Info("object is not type of AdmissionPolicy: %+v", policy)
				return []ctrl.Request{}
			}

			return []ctrl.Request{
				{
					NamespacedName: client.ObjectKey{
						Name: policy.Spec.PolicyServer,
					},
				},
			}
		})).
		Watches(&source.Kind{Type: &policiesv1alpha2.ClusterAdmissionPolicy{}}, handler.EnqueueRequestsFromMapFunc(func(object client.Object) []reconcile.Request {
			// The watch will trigger twice per object change; once with the old
			// object, and once the new object. We need to be mindful when doing
			// Updates since they will invalidate the newever versions of the
			// object.
			policy, ok := object.(*policiesv1alpha2.ClusterAdmissionPolicy)
			if !ok {
				r.Log.Info("object is not type of ClusterAdmissionPolicy: %+v", policy)
				return []ctrl.Request{}
			}

			return []ctrl.Request{
				{
					NamespacedName: client.ObjectKey{
						Name: policy.Spec.PolicyServer,
					},
				},
			}
		})).
		Complete(r)

	return errors.Wrap(err, "failed enrolling controller with manager")
}
