/*
Copyright 2025.

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

package sequential

import (
	"context"

	"github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	namespaceFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/namespace"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-052_validate_rolebinding_number", func() {
		// TODO: check if this test can use a new ArgoCD instance instead of the default openshift-gitops instance

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies RoleBindings are added to namespace-scoped Namespace when that Namespace is managed by openshift-gitops", Label("openshift"), func() {

			By("creating and checking ArgoCD instance is available")
			namespace, cleanup := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanup()
			argocd := &v1beta1.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: namespace.Name},
			}
			Expect(k8sClient.Create(ctx, argocd)).To(Succeed())
			Eventually(argocd, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("creating simple namespace-scoped Argo CD instance")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			namespaceFixture.Update(ns, func(ns *corev1.Namespace) {
				ns.Labels["argocd.argoproj.io/managed-by"] = argocd.Namespace
			})

			roleBindingList := []string{argocd.Name + "-argocd-application-controller",
				argocd.Name + "-argocd-server"}

			for _, rb := range roleBindingList {
				rb := &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: rb, Namespace: ns.Name},
				}
				Eventually(rb, "3m", "1s").Should(k8sFixture.ExistByName())
			}

			for _, rb := range roleBindingList {
				rb := &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: rb, Namespace: ns.Name},
				}
				Consistently(rb, "20s", "4s").Should(k8sFixture.ExistByName())
			}

		})

	})
})
