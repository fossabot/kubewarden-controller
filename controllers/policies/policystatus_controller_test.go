package policies

import (
	"context"
	"encoding/json"
	policiesv1alpha2 "github.com/kubewarden/kubewarden-controller/apis/policies/v1alpha2"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/admission"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/constants"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/naming"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var policyServer = policiesv1alpha2.PolicyServer{
	ObjectMeta: metav1.ObjectMeta{
		Name: policyServerName,
	},
	Spec: policiesv1alpha2.PolicyServerSpec{
		Image:    "ghcr.io/kubewarden/policy-server",
		Replicas: 1,
	},
}

var policyServerDeployment = appsv1.Deployment{
	ObjectMeta: metav1.ObjectMeta{
		Name:      naming.PolicyServerDeploymentNameForPolicyServerName(policyServerName),
		Namespace: namespace,
	},
	Spec: appsv1.DeploymentSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				constants.AppLabelKey: policyServer.AppLabel(),
			},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					constants.AppLabelKey: policyServer.AppLabel(),
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					corev1.Container{Name: "name", Image: "image"},
				},
			},
		},
	},
}

/*
Given an AdmissionPolicy
When does not have a PolicyServer
Then status is unscheduled
*/
func verifyPolicyUnscheduled(ctx context.Context, policy policiesv1alpha2.Policy) {
	defer func() {
		Expect(k8sClient.Delete(ctx, policy))
	}()
	Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

	lookupKey := types.NamespacedName{Name: policy.GetName(), Namespace: policy.GetNamespace()}

	Eventually(func() bool {
		err := k8sClient.Get(ctx, lookupKey, policy)
		if err != nil {
			return false
		}
		return policy.GetStatus().PolicyStatus == policiesv1alpha2.PolicyStatusUnscheduled
	}, timeout, interval).Should(BeTrue())
}

/*
Given an AdmissionPolicy
When has a PolicyServer
Then status is scheduled
And when PolicyServer is created
Then status is pending
*/
func verifyPolicyTransitionToPending(ctx context.Context, policy policiesv1alpha2.Policy) {
	defer func() {
		Expect(k8sClient.Delete(ctx, policy)).Should(Succeed())
	}()
	Expect(k8sClient.Create(ctx, policy)).Should(Succeed())
	lookupKey := types.NamespacedName{Name: policy.GetName(), Namespace: policy.GetNamespace()}

	Eventually(func() bool {
		err := k8sClient.Get(ctx, lookupKey, policy)
		if err != nil {
			return false
		}
		return policy.GetStatus().PolicyStatus == policiesv1alpha2.PolicyStatusScheduled
	}, timeout, interval).Should(BeTrue())

	Expect(k8sClient.Create(ctx, policyServer.DeepCopy())).Should(Succeed())
	defer func() {
		Expect(k8sClient.Delete(ctx, policyServer.DeepCopy())).Should(Succeed())
	}()

	Eventually(func() bool {
		err := k8sClient.Get(ctx, lookupKey, policy)
		if err != nil {
			return false
		}
		return policy.GetStatus().PolicyStatus == policiesv1alpha2.PolicyStatusPending
	}, timeout, interval).Should(BeTrue())
}

/*
Given an AdmissionPolicy in monitor mode
When has a PolicyServer
Then mode is monitor
And mode is changed in configmap
Then policy mode is updated
*/
func verifyPolicyModeTransition(ctx context.Context, policy policiesv1alpha2.Policy) {
	lookupKey := types.NamespacedName{Name: policy.GetName(), Namespace: policy.GetNamespace()}

	Eventually(func() bool {
		err := k8sClient.Get(ctx, lookupKey, policy)
		if err != nil {
			return false
		}
		return policy.GetStatus().PolicyMode == policiesv1alpha2.PolicyModeStatusMonitor
	}, timeout, interval).Should(BeTrue())

	cfg := createConfigMap(policy, string(policiesv1alpha2.PolicyModeStatusProtect))
	Expect(k8sClient.Update(ctx, cfg)).Should(Succeed())

	Eventually(func() bool {
		err := k8sClient.Get(ctx, lookupKey, policy)
		if err != nil {
			return false
		}
		return policy.GetStatus().PolicyMode == policiesv1alpha2.PolicyModeStatusProtect
	}, timeout, interval).Should(BeTrue())

}

/*
Given an AdmissionPolicy
When has a PolicyServer
Then PolicyServerConfigurationUpToDate is false and reason is ConfigurationVersionMismatch
And deployment PolicyServerDeploymentConfigVersionAnnotation annotation is updated with cm ResourceVersion
Then PolicyServerConfigurationUpToDate is true and reason is ConfigurationVersionMatch
*/
func verifyConfigurationVersionMatch(ctx context.Context, policy policiesv1alpha2.Policy) {
	lookupKey := types.NamespacedName{Name: policy.GetName(), Namespace: policy.GetNamespace()}

	Eventually(func() bool {
		err := k8sClient.Get(ctx, lookupKey, policy)
		if err != nil {
			return false
		}
		condition := getPolicyServerConfigurationUpToDateCondition(policy.GetStatus().Conditions)
		if condition == nil {
			return false
		}
		return condition.Status == metav1.ConditionFalse && condition.Reason == "ConfigurationVersionMismatch"
	}, timeout, interval).Should(BeTrue())

	deployment := policyServerDeployment.DeepCopy()
	cm := createConfigMap(policy, string(policiesv1alpha2.PolicyModeStatusMonitor))
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: policyServer.NameWithPrefix(), Namespace: policy.GetNamespace()}, cm)).Should(Succeed())
	deployment.Annotations = map[string]string{constants.PolicyServerDeploymentConfigVersionAnnotation: cm.ResourceVersion}
	Expect(k8sClient.Update(ctx, deployment)).Should(Succeed())

	Eventually(func() bool {
		err := k8sClient.Get(ctx, lookupKey, policy)
		if err != nil {
			return false
		}
		condition := getPolicyServerConfigurationUpToDateCondition(policy.GetStatus().Conditions)

		if condition == nil {
			return false
		}
		return condition.Status == metav1.ConditionTrue && condition.Reason == "ConfigurationVersionMatch"
	}, timeout, interval).Should(BeTrue())
}

func getPolicyServerConfigurationUpToDateCondition(conditions []metav1.Condition) *metav1.Condition {
	for _, condition := range conditions {
		if condition.Type == string(policiesv1alpha2.PolicyServerConfigurationUpToDate) {
			return &condition
		}
	}
	return nil
}

func createConfigMap(policy policiesv1alpha2.Policy, mode string) *corev1.ConfigMap {
	entry := admission.PolicyConfigEntryMap{
		policy.GetUniqueName(): admission.PolicyServerConfigEntry{
			NamespacedName: types.NamespacedName{
				Namespace: namespace,
				Name:      policy.GetUniqueName(),
			},
			URL:        "test",
			PolicyMode: mode,
		},
	}
	policiesJSON, _ := json.Marshal(entry)

	data := map[string]string{
		constants.PolicyServerConfigPoliciesEntry: string(policiesJSON),
	}

	cfg := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyServer.NameWithPrefix(),
			Namespace: namespace,
			Labels: map[string]string{
				constants.PolicyServerLabelKey: policyServer.ObjectMeta.Name,
			},
		},
		Data: data,
	}

	return cfg
}
