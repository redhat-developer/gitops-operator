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

package parallel

import (
	"context"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	configmapFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/configmap"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-080_validate_regex_support_argocd_rbac_test", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("ensures that updating ArgoCD CR spec.rbac.policyMatcherMode causes the value to be correctly set on argocd-rbac-cm ConfigMap", func() {

			By("creating a basic Argo CD instance")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-argocd",
					Namespace: ns.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			By("set regex Policy Matcher Mode")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.RBAC.PolicyMatcherMode = ptr.To("regex")
			})

			By("verifying it gets set on argocd-rbac-cm ConfigMap")
			argocdRBACCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-rbac-cm",
					Namespace: ns.Name,
				},
			}
			Eventually(argocdRBACCM).Should(k8sFixture.ExistByName())
			Eventually(argocdRBACCM).Should(configmapFixture.HaveStringDataKeyValue("policy.matchMode", "regex"))

			By("updating the ConfigMap manually to an invalid value. We don't support users modifying this value manully.")
			configmapFixture.Update(argocdRBACCM, func(cm *corev1.ConfigMap) {
				cm.Data["policy.matchMode"] = ""
			})

			By("verifying the ConfigMap is reconciled back to the value specified in the ArgoCD CR")
			Eventually(argocdRBACCM).Should(configmapFixture.HaveStringDataKeyValue("policy.matchMode", "regex"))
			Consistently(argocdRBACCM).Should(configmapFixture.HaveStringDataKeyValue("policy.matchMode", "regex"))

			By("verifying we can also set glob, and it is set in the ConfigMap")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.RBAC.PolicyMatcherMode = ptr.To("glob")
			})
			Eventually(argocdRBACCM).Should(configmapFixture.HaveStringDataKeyValue("policy.matchMode", "glob"))

		})

	})
})
