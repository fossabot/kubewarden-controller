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

/*
import (
	"context"
	"github.com/kubewarden/kubewarden-controller/internal/pkg/admission"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"path/filepath"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"testing"

	policiesv1alpha2 "github.com/kubewarden/kubewarden-controller/apis/policies/v1alpha2"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

/*
var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})*/

/*
func verifyPolicyDeletion() {
	Expect(k8sClient.Create(ctx, policyServer.DeepCopy())).Should(Succeed())
	Expect(k8sClient.Create(ctx, admissionPolicyWithPolicyServer.DeepCopy())).Should(Succeed())

	Expect(k8sClient.Delete(ctx, policyServer.DeepCopy())).Should(Succeed())

	lookupKey := types.NamespacedName{Name: admissionPolicyWithPolicyServer.GetName(), Namespace: admissionPolicyWithPolicyServer.GetNamespace()}

	Eventually(func() bool {
		err := k8sClient.Get(ctx, lookupKey, &admissionPolicyWithPolicyServer)
		return errors.IsNotFound(err)
	}, timeout, interval).Should(BeTrue())

}*/
