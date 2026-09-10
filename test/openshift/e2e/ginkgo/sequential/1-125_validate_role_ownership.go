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
	argocdFixture "github.com/argoproj-labs/argocd-operator/tests/ginkgo/fixture/argocd"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-125_validate_role_ownership", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)
		const (
			applicationControllerClusterRoleName           = "openshift-gitops-openshift-gitops-argocd-application-controller"
			applicationSetControllerClusterRoleName        = "openshift-gitops-openshift-gitops-argocd-applicationset-controller"
			serverClusterRoleName                          = "openshift-gitops-openshift-gitops-argocd-server"
			applicationControllerClusterRoleBindingName    = "openshift-gitops-openshift-gitops-argocd-application-controller"
			applicationSetControllerClusterRoleBindingName = "openshift-gitops-openshift-gitops-argocd-applicationset-controller"
			serverClusterRoleBindingName                   = "openshift-gitops-openshift-gitops-argocd-server"
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("validates that namespace-scoped resources do not delete a ClusterRole or ClusterRoleBinding with a matching generated name", func() {

			By("checking that the default ClusterRole and clusterroleBinding for the ArgoCD Application Controller and Server exists")
			defaultControllerClusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: applicationControllerClusterRoleName,
				},
			}
			defaultApplicationSetControllerClusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: applicationSetControllerClusterRoleName,
				},
			}
			defaultServerClusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: serverClusterRoleName,
				},
			}
			defaultControllerClusterRoleBinding := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: applicationControllerClusterRoleBindingName,
				},
			}
			defaultApplicationSetControllerClusterRoleBinding := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: applicationSetControllerClusterRoleBindingName,
				},
			}
			defaultServerClusterRoleBinding := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: serverClusterRoleBindingName,
				},
			}
			Eventually(defaultControllerClusterRole).Should(k8sFixture.ExistByName())
			Eventually(defaultApplicationSetControllerClusterRole).Should(k8sFixture.ExistByName())
			Eventually(defaultServerClusterRole).Should(k8sFixture.ExistByName())
			Eventually(defaultControllerClusterRoleBinding).Should(k8sFixture.ExistByName())
			Eventually(defaultApplicationSetControllerClusterRoleBinding).Should(k8sFixture.ExistByName())
			Eventually(defaultServerClusterRoleBinding).Should(k8sFixture.ExistByName())

			By("fetching initial UID of the clusterrole")
			initialControllerUid := defaultControllerClusterRole.GetUID()
			initialApplicationSetControllerUid := defaultApplicationSetControllerClusterRole.GetUID()
			initialServerUid := defaultServerClusterRole.GetUID()
			initialControllerRoleBindingUid := defaultControllerClusterRoleBinding.GetUID()
			initialApplicationSetControllerRoleBindingUid := defaultApplicationSetControllerClusterRoleBinding.GetUID()
			initialServerRoleBindingUid := defaultServerClusterRoleBinding.GetUID()

			defaultArgocd := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops",
					Namespace: "openshift-gitops",
				},
			}

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(defaultArgocd, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("creating new namespace scoped ArgoCD instance to create the condition where clusterrole and clusterrolebinding are deleted by namespaced scoped resources")
			ns, nsCleanup := fixture.CreateNamespaceWithCleanupFunc("gitops")
			defer nsCleanup()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-openshift",
					Namespace: ns.Name,
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("checking that the default ClusterRole for the ArgoCD Application Controller still exists")
			afterReconcileControllerClusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: applicationControllerClusterRoleName,
				},
			}
			afterReconcileApplicationSetControllerCR := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: applicationSetControllerClusterRoleName,
				},
			}
			afterReconcileServerCR := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: serverClusterRoleName,
				},
			}
			afterReconcileControllerCRB := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: applicationControllerClusterRoleBindingName,
				},
			}
			afterReconcileApplicationSetControllerCRB := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: applicationSetControllerClusterRoleBindingName,
				},
			}
			afterReconcileServerCRB := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: serverClusterRoleBindingName,
				},
			}

			Eventually(afterReconcileControllerClusterRole).Should(k8sFixture.ExistByName())
			Eventually(afterReconcileApplicationSetControllerCR).Should(k8sFixture.ExistByName())
			Eventually(afterReconcileServerCR).Should(k8sFixture.ExistByName())

			Eventually(afterReconcileControllerCRB).Should(k8sFixture.ExistByName())
			Eventually(afterReconcileApplicationSetControllerCRB).Should(k8sFixture.ExistByName())
			Eventually(afterReconcileServerCRB).Should(k8sFixture.ExistByName())

			By("fetching UID of the clusterrole after reconciliation")
			afterReconcileControllerUid := afterReconcileControllerClusterRole.GetUID()
			afterReconcileApplicationSetControllerUid := afterReconcileApplicationSetControllerCR.GetUID()
			afterReconcileServerUid := afterReconcileServerCR.GetUID()

			afterReconcileControllerRBUid := afterReconcileControllerCRB.GetUID()
			afterReconcileApplicationSetControllerRBUid := afterReconcileApplicationSetControllerCRB.GetUID()
			afterReconcileServerRBUid := afterReconcileServerCRB.GetUID()

			By("comparing the UID to ensure that the ClusterRole and ClusterRoleBinding are not recreated")
			Expect(initialControllerUid).To(Equal(afterReconcileControllerUid), "the ClusterRole was recreated")
			Expect(initialApplicationSetControllerUid).To(Equal(afterReconcileApplicationSetControllerUid), "the ClusterRole was recreated")
			Expect(initialServerUid).To(Equal(afterReconcileServerUid), "the ClusterRole was recreated")

			Expect(initialControllerRoleBindingUid).To(Equal(afterReconcileControllerRBUid))
			Expect(initialApplicationSetControllerRoleBindingUid).To(Equal(afterReconcileApplicationSetControllerRBUid))
			Expect(initialServerRoleBindingUid).To(Equal(afterReconcileServerRBUid))

		})

	})
})
