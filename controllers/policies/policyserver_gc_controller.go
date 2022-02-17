package policies

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	policiesv1alpha2 "github.com/kubewarden/kubewarden-controller/apis/policies/v1alpha2"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/admission"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/constants"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// PolicyServerGCReconciler removes policy server Kubewarden finalizer
// that are no longer attached to any existing policy
type PolicyServerGCReconciler struct {
	client.Client
	Log        logr.Logger
	Scheme     *runtime.Scheme
	Reconciler admission.Reconciler
}

func (r *PolicyServerGCReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	policyServer := policiesv1alpha2.PolicyServer{}
	if err := r.Reconciler.APIReader.Get(ctx, req.NamespacedName, &policyServer); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("cannot retrieve policy server: %w", err)
	}
	if policyServer.DeletionTimestamp == nil {
		// If the policy server is not deleted, there is nothing to
		// reconcile
		return ctrl.Result{}, nil
	}
	clusterAdmissionPolicyList := policiesv1alpha2.ClusterAdmissionPolicyList{}
	if err := r.Reconciler.APIReader.List(ctx, &clusterAdmissionPolicyList); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot list cluster admission policies: %w", err)
	}
	for _, policy := range clusterAdmissionPolicyList.Items {
		if policy.Spec.PolicyServer == policyServer.Name {
			// Return and requeue, since this Policy Server has at least one
			// cluster admission policy bound
			return ctrl.Result{Requeue: true}, nil
		}
	}
	admissionPolicyList := policiesv1alpha2.AdmissionPolicyList{}
	if err := r.Reconciler.APIReader.List(ctx, &admissionPolicyList); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot list admission policies: %w", err)
	}
	for _, policy := range admissionPolicyList.Items {
		if policy.Spec.PolicyServer == policyServer.Name {
			// Return and requeue, since this Policy Server has at least one
			// cluster admission policy bound
			return ctrl.Result{Requeue: true}, nil
		}
	}
	controllerutil.RemoveFinalizer(&policyServer, constants.KubewardenFinalizer)
	fmt.Println("=> update status 2")
	if err := r.Update(context.TODO(), &policyServer); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update policy server: %w", err)
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PolicyServerGCReconciler) SetupWithManager(mgr ctrl.Manager) error {
	err := ctrl.NewControllerManagedBy(mgr).
		For(&policiesv1alpha2.PolicyServer{}).
		Watches(&source.Kind{Type: &policiesv1alpha2.ClusterAdmissionPolicy{}}, handler.Funcs{
			DeleteFunc: func(e event.DeleteEvent, queue workqueue.RateLimitingInterface) {
				policy, ok := e.Object.(*policiesv1alpha2.ClusterAdmissionPolicy)
				if !ok {
					r.Log.Info("object is not type of ClusterAdmissionPolicy: %+v", policy)
					return
				}
				queue.Add(ctrl.Request{
					NamespacedName: client.ObjectKey{Name: policy.Spec.PolicyServer},
				})
			},
		}).
		Watches(&source.Kind{Type: &policiesv1alpha2.AdmissionPolicy{}}, handler.Funcs{
			DeleteFunc: func(e event.DeleteEvent, queue workqueue.RateLimitingInterface) {
				policy, ok := e.Object.(*policiesv1alpha2.AdmissionPolicy)
				if !ok {
					r.Log.Info("object is not type of AdmissionPolicy: %+v", policy)
					return
				}
				queue.Add(ctrl.Request{
					NamespacedName: client.ObjectKey{Name: policy.Spec.PolicyServer},
				})
			},
		}).
		Complete(r)

	if err != nil {
		err = fmt.Errorf("failed enrolling controller with manager: %w", err)
	}
	return err
}
