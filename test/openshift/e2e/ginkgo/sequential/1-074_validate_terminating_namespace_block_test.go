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

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	configmapFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/configmap"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-074_validate_terminating_namespace_block", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("ensures that if one managed namespace is stuck in deleting state -- due to a finalizer -- it does not block other managed namespaces from being reconciled", func() {

			By("creating an Argo CD instance that will manage other namespaces")
			gitops_2242_ns_main, cleanupFunc := fixture.CreateNamespaceWithCleanupFunc("gitops-2242-ns-main")
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "gitops-2242-argocd",
					Namespace:  gitops_2242_ns_main.Name,
					Finalizers: []string{"argoproj.io/finalizer"},
				},
				Spec: argov1beta1api.ArgoCDSpec{
					RBAC: argov1beta1api.ArgoCDRBACSpec{
						Policy: ptr.To("g, system:authenticated, role:admin"),
						Scopes: ptr.To("[groups]"),
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			// This RoleBinding (ported from kuttl test) appears not to have a point: it grants openshift-gitops argo cd instance to this argo cd.
			// I've left it commented out.
			//
			// nsRoleBinding := &rbacv1.RoleBinding{
			// 	ObjectMeta: metav1.ObjectMeta{
			// 		Name:      "grant-argocd",
			// 		Namespace: gitops_2242_ns_main.Name,
			// 	},
			// 	RoleRef: rbacv1.RoleRef{
			// 		APIGroup: "rbac.authorization.k8s.io",
			// 		Kind:     "ClusterRole",
			// 		Name:     "admin",
			// 	},
			// 	Subjects: []rbacv1.Subject{
			// 		{Kind: "ServiceAccount", Name: "openshift-gitops-argocd-application-controller", Namespace: "openshift-gitops"}},
			// }
			// Expect(k8sClient.Create(ctx, nsRoleBinding)).To(Succeed())

			gitops_2242_ns_first, cleanupFunc := fixture.CreateManagedNamespaceWithCleanupFunc("gitops-2242-ns-first", gitops_2242_ns_main.Name)
			defer cleanupFunc()

			By("creating a ConfigMap with a finalizer, so that when the Namespace is deleted, the Namespace cannot finish deleting until the ConfigMap finalizer is removed")
			firstConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-config-map-2",
					Namespace: gitops_2242_ns_first.Name,
					Finalizers: []string{
						"some.random/finalizer",
					},
				},
				Data: map[string]string{
					"foo": "bar",
				},
			}
			Expect(k8sClient.Create(ctx, firstConfigMap)).To(Succeed())
			defer func() {
				// At the end of the test, remove the finalizer from the ConfigMap so the NS can be deleted.
				configmapFixture.Update(firstConfigMap, func(cm *corev1.ConfigMap) {
					cm.Finalizers = nil
				})
			}()

			By("starting to delete the Namespace in the background. This puts the Namespace into deletion state, but it cannot finish deletion until the ConfigMap has its finalizer removed, which happens at the end of the test")
			go func() {
				defer GinkgoRecover()
				Expect(k8sClient.Delete(ctx, gitops_2242_ns_first)).To(Succeed())
			}()

			By("creating a second managed namespace, to managed by the Argo CD instance")
			gitops_2242_ns_second, cleanupFunc := fixture.CreateManagedNamespaceWithCleanupFunc("gitops-2242-ns-second", "gitops-2242-ns-main")
			defer cleanupFunc()

			By("verifying that the operator is successfully able to grant access from the Argo CD instance to the second Namespace. That confirms that, even though the first namespace is in deletion state, that the operator is not blocked on other Namespaces")
			argocdServerRoleBindingInGitops_2242_ns_second := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "gitops-2242-argocd-argocd-server", Namespace: gitops_2242_ns_second.Name},
			}
			Eventually(argocdServerRoleBindingInGitops_2242_ns_second).Should(k8sFixture.ExistByName())

			argocdApplicationServerRoleBindingInGitops_2242_ns_second := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "gitops-2242-argocd-argocd-application-controller", Namespace: gitops_2242_ns_second.Name},
			}
			Eventually(argocdApplicationServerRoleBindingInGitops_2242_ns_second).Should(k8sFixture.ExistByName())

		})

	})
})
