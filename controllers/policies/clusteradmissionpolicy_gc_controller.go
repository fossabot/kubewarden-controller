package policies

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/kubewarden/kubewarden-controller/apis/policies/v1alpha2"
	policiesv1alpha2 "github.com/kubewarden/kubewarden-controller/apis/policies/v1alpha2"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/admission"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ClusterAdmissionPolicyGCReconciler deletes cluster-wide policies
// that are no longer attached to any policy server
type ClusterAdmissionPolicyGCReconciler struct {
	client.Client
	Log        logr.Logger
	Scheme     *runtime.Scheme
	Reconciler admission.Reconciler
}

func (r *ClusterAdmissionPolicyGCReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	policy := v1alpha2.ClusterAdmissionPolicy{}
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("cannot retrieve admission policy: %w", err)
	}
	return gcPolicy(ctx, r.Reconciler.Client, &policy)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterAdmissionPolicyGCReconciler) SetupWithManager(mgr ctrl.Manager) error {
	err := ctrl.NewControllerManagedBy(mgr).
		For(&policiesv1alpha2.ClusterAdmissionPolicy{}).
		Watches(&source.Kind{Type: &policiesv1alpha2.PolicyServer{}}, handler.Funcs{
			UpdateFunc: func(e event.UpdateEvent, queue workqueue.RateLimitingInterface) {
				// When a policy server is deleted, the first thing that
				// happens is that it is updated by setting the
				// DeletionTimestamp. We trigger a reconciliation of all the
				// related cluster admission policies, so we can GC them. When
				// no more policies are linked to the policy server, the
				// Policy Server GC will remove its finalizer, and Kubernetes
				// will GC it.
				if e.ObjectNew.GetDeletionTimestamp() == nil {
					return
				}
				clusterAdmissionPolicyList := policiesv1alpha2.ClusterAdmissionPolicyList{}
				if err := r.Reconciler.APIReader.List(context.TODO(), &clusterAdmissionPolicyList); err != nil {
					return
				}
				policyServerName := e.ObjectNew.GetName()
				for _, clusterAdmissionPolicy := range clusterAdmissionPolicyList.Items {
					if clusterAdmissionPolicy.Spec.PolicyServer == policyServerName {
						queue.Add(ctrl.Request{NamespacedName: client.ObjectKey{Name: clusterAdmissionPolicy.GetName()}})
					}
				}
			},
		}).
		Complete(r)

	if err != nil {
		err = fmt.Errorf("failed enrolling controller with manager: %w", err)
	}
	return err
}
