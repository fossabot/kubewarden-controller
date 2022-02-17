package admission

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/admissionregistration"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/constants"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/metrics"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	policiesv1alpha2 "github.com/kubewarden/kubewarden-controller/apis/policies/v1alpha2"
)

type Reconciler struct {
	Client               client.Client
	APIReader            client.Reader
	DeploymentsNamespace string
	Log                  logr.Logger
}

type reconcilerErrors []error

func (errorList reconcilerErrors) Error() string {
	errors := []string{}
	for _, error := range errorList {
		errors = append(errors, error.Error())
	}
	return strings.Join(errors, ", ")
}

func (r *Reconciler) ReconcileDeletion(
	ctx context.Context,
	policyServer *policiesv1alpha2.PolicyServer,
) error {
	errors := reconcilerErrors{}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyServer.NameWithPrefix(),
			Namespace: r.DeploymentsNamespace,
		},
	}
	err := r.Client.Delete(ctx, deployment)
	if err == nil {
		setFalseConditionType(
			&policyServer.Status.Conditions,
			string(policiesv1alpha2.PolicyServerDeploymentReconciled),
			"Policy Server has been deleted",
		)
	} else if !apierrors.IsNotFound(err) {
		r.Log.Error(err, "ReconcileDeletion: cannot delete PolicyServer Deployment "+policyServer.Name)
		errors = append(errors, err)
	}

	certificateSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyServer.NameWithPrefix(),
			Namespace: r.DeploymentsNamespace,
		},
	}
	err = r.Client.Delete(ctx, certificateSecret)
	if err == nil {
		setFalseConditionType(
			&policyServer.Status.Conditions,
			string(policiesv1alpha2.PolicyServerCASecretReconciled),
			"Policy Server has been deleted",
		)
	} else if !apierrors.IsNotFound(err) {
		r.Log.Error(err, "ReconcileDeletion: cannot delete PolicyServer Certificate Secret "+policyServer.Name)
		errors = append(errors, err)
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyServer.NameWithPrefix(),
			Namespace: r.DeploymentsNamespace,
		},
	}
	err = r.Client.Delete(ctx, service)
	if err == nil {
		setFalseConditionType(
			&policyServer.Status.Conditions,
			string(policiesv1alpha2.PolicyServerServiceReconciled),
			"Policy Server has been deleted",
		)
	} else if !apierrors.IsNotFound(err) {
		r.Log.Error(err, "ReconcileDeletion: cannot delete PolicyServer Service "+policyServer.Name)
		errors = append(errors, err)
	}

	cfg := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyServer.NameWithPrefix(),
			Namespace: r.DeploymentsNamespace,
		},
	}
	err = r.Client.Delete(ctx, cfg)
	if err == nil {
		setFalseConditionType(
			&policyServer.Status.Conditions,
			string(policiesv1alpha2.PolicyServerConfigMapReconciled),
			"Policy Server has been deleted",
		)
	} else if !apierrors.IsNotFound(err) {
		r.Log.Error(err, "ReconcileDeletion: cannot delete PolicyServer ConfigMap "+policyServer.Name)
		errors = append(errors, err)
	}

	if len(errors) == 0 {
		return nil
	}

	return errors
}

func setFalseConditionType(
	conditions *[]metav1.Condition,
	conditionType string,
	message string,
) {
	apimeta.SetStatusCondition(
		conditions,
		metav1.Condition{
			Type:    conditionType,
			Status:  metav1.ConditionFalse,
			Reason:  string(policiesv1alpha2.ReconciliationFailed),
			Message: message,
		},
	)
}

func setTrueConditionType(conditions *[]metav1.Condition, conditionType string) {
	apimeta.SetStatusCondition(
		conditions,
		metav1.Condition{
			Type:   conditionType,
			Status: metav1.ConditionTrue,
			Reason: string(policiesv1alpha2.ReconciliationSucceeded),
		},
	)
}

func (r *Reconciler) Reconcile(
	ctx context.Context,
	policyServer *policiesv1alpha2.PolicyServer,
) error {
	policies, err := r.getPolicies(ctx, policyServer)
	if err != nil {
		return fmt.Errorf("cannot retrieve policies: %w", err)
	}

	err = r.deleteWebhooksClusterAdmissionPolicies(ctx, policies)
	if err != nil {
		return fmt.Errorf("cannot delete cluster admission policies: %w", err)
	}

	policyServerCARootSecret, err := r.fetchOrInitializePolicyServerCARootSecret(ctx, admissionregistration.GenerateCA, admissionregistration.PemEncodeCertificate)
	if err != nil {
		setFalseConditionType(
			&policyServer.Status.Conditions,
			string(policiesv1alpha2.PolicyServerCARootSecretReconciled),
			fmt.Sprintf("error reconciling secret: %v", err),
		)
		return err
	}

	if err := r.reconcileCASecret(ctx, policyServerCARootSecret); err != nil {
		setFalseConditionType(
			&policyServer.Status.Conditions,
			string(policiesv1alpha2.PolicyServerCARootSecretReconciled),
			fmt.Sprintf("error reconciling secret: %v", err),
		)
		return err
	}

	setTrueConditionType(
		&policyServer.Status.Conditions,
		string(policiesv1alpha2.PolicyServerCARootSecretReconciled),
	)

	policyServerCASecret, err := r.fetchOrInitializePolicyServerCASecret(ctx, policyServer.NameWithPrefix(), policyServerCARootSecret, admissionregistration.GenerateCert)
	if err != nil {
		setFalseConditionType(
			&policyServer.Status.Conditions,
			string(policiesv1alpha2.PolicyServerCASecretReconciled),
			fmt.Sprintf("error reconciling secret: %v", err),
		)
		return err
	}

	if err := r.reconcileCASecret(ctx, policyServerCASecret); err != nil {
		setFalseConditionType(
			&policyServer.Status.Conditions,
			string(policiesv1alpha2.PolicyServerCASecretReconciled),
			fmt.Sprintf("error reconciling secret: %v", err),
		)
		return err
	}

	setTrueConditionType(
		&policyServer.Status.Conditions,
		string(policiesv1alpha2.PolicyServerCASecretReconciled),
	)

	policies, err = r.getPolicies(ctx, policyServer)
	if err != nil {
		return fmt.Errorf("cannot retrieve cluster admission policies: %w", err)
	}

	if err := r.reconcilePolicyServerConfigMap(ctx, policyServer, policies); err != nil {
		setFalseConditionType(
			&policyServer.Status.Conditions,
			string(policiesv1alpha2.PolicyServerConfigMapReconciled),
			fmt.Sprintf("error reconciling configmap: %v", err),
		)
		return err
	}

	setTrueConditionType(
		&policyServer.Status.Conditions,
		string(policiesv1alpha2.PolicyServerConfigMapReconciled),
	)

	if err := r.reconcilePolicyServerDeployment(ctx, policyServer); err != nil {
		setFalseConditionType(
			&policyServer.Status.Conditions,
			string(policiesv1alpha2.PolicyServerDeploymentReconciled),
			fmt.Sprintf("error reconciling deployment: %v", err),
		)
		return err
	}

	setTrueConditionType(
		&policyServer.Status.Conditions,
		string(policiesv1alpha2.PolicyServerDeploymentReconciled),
	)

	if err := r.reconcilePolicyServerService(ctx, policyServer); err != nil {
		setFalseConditionType(
			&policyServer.Status.Conditions,
			string(policiesv1alpha2.PolicyServerServiceReconciled),
			fmt.Sprintf("error reconciling service: %v", err),
		)
		return err
	}

	setTrueConditionType(
		&policyServer.Status.Conditions,
		string(policiesv1alpha2.PolicyServerServiceReconciled),
	)

	return r.enablePolicyWebhook(ctx, policyServer, policyServerCARootSecret, policies)
}

func (r *Reconciler) HasClusterAdmissionPoliciesBounded(ctx context.Context, policyServer *policiesv1alpha2.PolicyServer) (bool, error) {
	policies, err := r.getPolicies(ctx, policyServer)
	if err != nil {
		return false, err
	}
	if len(policies) > 0 {
		return true, nil
	}

	return false, nil
}

func (r *Reconciler) enablePolicyWebhook(
	ctx context.Context,
	policyServer *policiesv1alpha2.PolicyServer,
	policyServerSecret *corev1.Secret,
	policies []policiesv1alpha2.Policy) error {
	policyServerReady, err := r.isPolicyServerReady(ctx, policyServer)
	if err != nil {
		return err
	}
	if !policyServerReady {
		return errors.New("policy server not yet ready")
	}
	for _, policy := range policies {
		if policy.IsMutating() {
			if err := r.reconcileMutatingWebhookConfiguration(ctx, policy, policyServerSecret, policyServer.NameWithPrefix()); err != nil {
				setFalseConditionType(
					&policy.GetStatus().Conditions,
					string(policiesv1alpha2.PolicyActive),
					fmt.Sprintf("error reconciling mutating webhook configuration: %v", err),
				)
				return err
			}
		} else {
			if err := r.reconcileValidatingWebhookConfiguration(ctx, policy, policyServerSecret, policyServer.NameWithPrefix()); err != nil {
				setFalseConditionType(
					&policy.GetStatus().Conditions,
					string(policiesv1alpha2.PolicyActive),
					fmt.Sprintf("error reconciling validating webhook configuration: %v", err),
				)
				return err
			}
		}
		setTrueConditionType(
			&policy.GetStatus().Conditions,
			string(policiesv1alpha2.PolicyActive),
		)
		policy.SetStatus(policiesv1alpha2.PolicyStatusActive)
		if err := r.UpdateAdmissionPolicyStatus(ctx, policy); err != nil {
			return err
		}
		r.Log.Info("Policy " + policy.GetUniqueName() + " active")
	}

	return nil
}

func (r *Reconciler) getPolicies(ctx context.Context, policyServer *policiesv1alpha2.PolicyServer) ([]policiesv1alpha2.Policy, error) {
	var clusterAdmissionPolicies policiesv1alpha2.ClusterAdmissionPolicyList
	err := r.Client.List(ctx, &clusterAdmissionPolicies, client.MatchingFields{constants.PolicyServerIndexKey: policyServer.Name})
	if err != nil && apierrors.IsNotFound(err) {
		err = fmt.Errorf("failed obtaining ClusterAdmissionPolicies: %w", err)
		return nil, err
	}
	var admissionPolicies policiesv1alpha2.AdmissionPolicyList
	err = r.Client.List(ctx, &admissionPolicies, client.MatchingFields{constants.PolicyServerIndexKey: policyServer.Name})
	if err != nil && apierrors.IsNotFound(err) {
		err = fmt.Errorf("failed obtaining ClusterAdmissionPolicies: %w", err)
		return nil, err
	}

	policies := make([]policiesv1alpha2.Policy, 0)
	for _, clusterAdmissionPolicy := range clusterAdmissionPolicies.Items {
		clusterAdmissionPolicy := clusterAdmissionPolicy
		policies = append(policies, &clusterAdmissionPolicy)
	}
	for _, admissionPolicy := range admissionPolicies.Items {
		admissionPolicy := admissionPolicy
		policies = append(policies, &admissionPolicy)
	}

	return policies, nil
}

func (r *Reconciler) deleteWebhooksClusterAdmissionPolicies(ctx context.Context, policies []policiesv1alpha2.Policy) error {
	for _, policy := range policies {
		policy := policy // safely use pointer inside for
		if policy.GetDeletionTimestamp() != nil {
			if policy.IsMutating() {
				mutatingWebhook := &admissionregistrationv1.MutatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: policy.GetUniqueName(),
					},
				}
				err := r.Client.Delete(ctx, mutatingWebhook)
				if err != nil && !apierrors.IsNotFound(err) {
					return fmt.Errorf("failed deleting webhook of ClusterAdmissionPolicy %s: %w",
						policy.GetUniqueName(), err)
				}
			} else {
				validatingWebhook := &admissionregistrationv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: policy.GetUniqueName(),
					},
				}
				err := r.Client.Delete(ctx, validatingWebhook)
				if err != nil && !apierrors.IsNotFound(err) {
					return fmt.Errorf("failed deleting webhook of Policy %s: %w",
						policy.GetUniqueName(), err)
				}
			}
			var patch policiesv1alpha2.Policy
			policy.CopyInto(&patch)
			controllerutil.RemoveFinalizer(patch, constants.KubewardenFinalizer)
			err := r.Client.Patch(ctx, patch, client.MergeFrom(policy))
			if err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed removing finalizers of Policy %s: %w",
					policy.GetUniqueName(), err)
			}
		}
	}
	return nil
}

// UpdateAdmissionPolicyStatus Updates the status subresource of the passed
// clusterAdmissionPolicy with a Client apt for it.
func (r *Reconciler) UpdateAdmissionPolicyStatus(
	ctx context.Context,
	policy policiesv1alpha2.Policy,
) error {
	if err := r.Client.Status().Update(ctx, policy); err != nil {
		return fmt.Errorf("failed to update status of Policy %q, %w", policy.GetObjectMeta(), err)
	}
	metrics.RecordPolicyCount(policy)
	return nil
}
