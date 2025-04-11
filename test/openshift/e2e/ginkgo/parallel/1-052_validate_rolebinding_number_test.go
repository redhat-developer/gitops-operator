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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	namespaceFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/namespace"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-052_validate_rolebinding_number", func() {

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
		})

		It("verifies RoleBindings are added to namespace-scoped Namespace when that Namespace is managed by openshift-gitops", func() {

			By("creating simple namespace-scoped Argo CD instance")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			namespaceFixture.Update(&ns, func(ns *corev1.Namespace) {
				ns.Labels["argocd.argoproj.io/managed-by"] = "openshift-gitops"
			})

			roleBindingList := []string{"openshift-gitops-argocd-application-controller",
				"openshift-gitops-argocd-server"}

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
