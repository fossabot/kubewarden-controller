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
	policiesv1alpha2 "github.com/kubewarden/kubewarden-controller/apis/policies/v1alpha2"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/admission"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/constants"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"path/filepath"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"testing"
	"time"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg       *rest.Config //nolint
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestPolicyServerController(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"AdmissionPolicy GC Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func() {

	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = policiesv1alpha2.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	var mgr manager.Manager
	Eventually(func() bool {
		mgr, err = ctrl.NewManager(cfg, ctrl.Options{
			Scheme: scheme.Scheme,
		})
		return err == nil
	}, timeout, interval).Should(BeTrue())

	Expect(err).ToNot(HaveOccurred())
	reconciler := admission.Reconciler{
		Client:               mgr.GetClient(),
		DeploymentsNamespace: "default",
	}

	err = (&PolicyServerReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		Log:        ctrl.Log.WithName("controllers").WithName("policies").WithName("ClusterAdmissionPolicy"),
		Reconciler: reconciler,
	}).SetupWithManager(mgr)

	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

}, 60)

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

const (
	admissionPolicyName = "admission-policy"
	policyServerName    = "policy-server"
	namespace           = "default"
	timeout             = time.Second * 10
	interval            = time.Millisecond * 250
)

var policyServer = policiesv1alpha2.PolicyServer{
	ObjectMeta: metav1.ObjectMeta{
		Name:      policyServerName,
		Namespace: namespace,
	},
	Spec: policiesv1alpha2.PolicyServerSpec{
		Image:    "ghcr.io/kubewarden/policy-server:v0.2.6",
		Replicas: 1,
	},
}

var admissionPolicyWithPolicyServer = policiesv1alpha2.AdmissionPolicy{
	ObjectMeta: metav1.ObjectMeta{
		Name:      admissionPolicyName,
		Namespace: namespace,
	},
	Spec: policiesv1alpha2.AdmissionPolicySpec{
		Rules:        make([]admissionregistrationv1.RuleWithOperations, 0),
		PolicyServer: policyServerName,
	},
	Status: policiesv1alpha2.PolicyStatus{},
}

var _ = Describe("Given an PolicyServer", func() {
	It("then policyServer CARootSecret is created", func() {
		Expect(k8sClient.Create(ctx, policyServer.DeepCopy())).Should(Succeed())
		defer func() {
			Expect(k8sClient.Delete(ctx, policyServer.DeepCopy())).Should(Succeed())
		}()
		caRootSecret := corev1.Secret{}
		Eventually(isObjectPresent(constants.PolicyServerCARootSecretName, &caRootSecret), timeout, interval).Should(BeTrue())
		Expect(caRootSecret.Data[constants.PolicyServerCARootCACert]).To(Not(BeNil()))
		Expect(caRootSecret.Data[constants.PolicyServerCARootPemName]).To(Not(BeNil()))
		Expect(caRootSecret.Data[constants.PolicyServerCARootPrivateKeyCertName]).To(Not(BeNil()))
		By("and CA secret is created with cert and key")
		certSecret := corev1.Secret{}
		Eventually(isObjectPresent(policyServer.NameWithPrefix(), &certSecret), timeout, interval).Should(BeTrue())
		Expect(certSecret.Data[constants.PolicyServerTLSCert]).To(Not(BeNil()))
		Expect(certSecret.Data[constants.PolicyServerTLSKey]).To(Not(BeNil()))
		defer func() {
			Expect(k8sClient.Delete(ctx, &certSecret)).Should(Succeed())
		}()

	})
})

func isObjectPresent(name string, object client.Object) bool {
	err := k8sClient.Get(
		ctx,
		client.ObjectKey{
			Namespace: namespace,
			Name:      name},
		object)
	return err == nil
}
