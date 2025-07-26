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
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	namespaceFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/namespace"
	osFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/os"
	rolebindingFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/rolebinding"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-083_validate_apps_in_any_namespace", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that namespaces added to .spec.sourceNamespaces are managed by openshift-gitops Argo CD instance, except when those namespaces also have managed-by label. Both addition and removal of values from this field are tested", func() {

			By("1) create test-1-24-custom namespace managed by openshift-gitops instance")

			test_1_24_customNS, cleanupFunc := fixture.CreateManagedNamespaceWithCleanupFunc("test-1-24-custom", "openshift-gitops")
			defer cleanupFunc()

			By("verifying openshift-gitops workloads exist and are running")

			openshiftGitOpsArgoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			Eventually(openshiftGitOpsArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			deploymentsToVerify := []string{
				"openshift-gitops-redis",
				"openshift-gitops-repo-server",
				"openshift-gitops-server",
				"openshift-gitops-applicationset-controller",
			}

			for _, deploymentToVerify := range deploymentsToVerify {
				depl := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      deploymentToVerify,
						Namespace: "openshift-gitops",
					},
				}
				Eventually(depl).Should(k8sFixture.ExistByName())
				Eventually(depl).Should(deploymentFixture.HaveReplicas(1))
				Eventually(depl, "2m", "5s").Should(deploymentFixture.HaveReadyReplicas(1))
			}

			appControllerSS := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-application-controller",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(appControllerSS).Should(k8sFixture.ExistByName())

			Eventually(test_1_24_customNS).Should(namespaceFixture.HaveLabel("argocd.argoproj.io/managed-by", "openshift-gitops"))

			ensureRolesAndRoleBindingsHaveExpectedValuesInTest1_2_24Namespace := func() {

				By("verifying that " + test_1_24_customNS.Name + " namespace has the expected server/app controller roles/rolebindings, and that the rolebindings grant access to openshift-gitops Argo CD instance")

				appControllerRole := &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "openshift-gitops-argocd-application-controller",
						Namespace: test_1_24_customNS.Name,
					},
				}
				Eventually(appControllerRole).Should(k8sFixture.ExistByName())

				serverRole := &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "openshift-gitops-argocd-server",
						Namespace: test_1_24_customNS.Name,
					},
				}
				Eventually(serverRole).Should(k8sFixture.ExistByName())

				appcontrollerRoleBinding := &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "openshift-gitops-argocd-application-controller",
						Namespace: test_1_24_customNS.Name,
					},
				}
				Eventually(appcontrollerRoleBinding).Should(k8sFixture.ExistByName())
				Eventually(appcontrollerRoleBinding).Should(rolebindingFixture.HaveRoleRef(rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "Role",
					Name:     "openshift-gitops-argocd-application-controller",
				}))
				Eventually(appcontrollerRoleBinding).Should(rolebindingFixture.HaveSubject(rbacv1.Subject{
					Kind:      "ServiceAccount",
					Name:      "openshift-gitops-argocd-application-controller",
					Namespace: "openshift-gitops",
				}))

				argocdServerRoleBinding := &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "openshift-gitops-argocd-server",
						Namespace: "test-1-24-custom",
					},
				}
				Eventually(argocdServerRoleBinding).Should(k8sFixture.ExistByName())
				Eventually(argocdServerRoleBinding).Should(rolebindingFixture.HaveRoleRef(rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "Role",
					Name:     "openshift-gitops-argocd-server",
				}))
				Eventually(argocdServerRoleBinding).Should(rolebindingFixture.HaveSubject(rbacv1.Subject{
					Kind:      "ServiceAccount",
					Name:      "openshift-gitops-argocd-server",
					Namespace: "openshift-gitops",
				}))

			}
			ensureRolesAndRoleBindingsHaveExpectedValuesInTest1_2_24Namespace()

			By("2) Adding 'test-1-24-custom' as a source NS to openshift-gitops .spec.sourceNamespaces")

			argocdFixture.Update(openshiftGitOpsArgoCD, func(ac *v1beta1.ArgoCD) {
				ac.Spec.SourceNamespaces = []string{
					"test-1-24-custom",
				}
			})

			By("verifying openshift-gitops instance should become ready")
			openshiftGitOpsServer := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-server",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(openshiftGitOpsServer).Should(k8sFixture.ExistByName())
			Eventually(openshiftGitOpsServer, "3m", "5s").Should(deploymentFixture.HaveReplicas(1))
			Eventually(openshiftGitOpsServer, "3m", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			openshiftGitOpsAppController := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-application-controller",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(openshiftGitOpsAppController).Should(k8sFixture.ExistByName())
			Eventually(openshiftGitOpsAppController).Should(statefulsetFixture.HaveReplicas(1))
			Eventually(openshiftGitOpsAppController).Should(statefulsetFixture.HaveReadyReplicas(1))

			By("verifing expected managed labels on test-1-24-custom, both managed-by and managed-by-cluster-argocd")
			Eventually(test_1_24_customNS).Should(namespaceFixture.HaveLabel("argocd.argoproj.io/managed-by", "openshift-gitops"))

			ensureRolesAndRoleBindingsHaveExpectedValuesInTest1_2_24Namespace()

			Eventually(test_1_24_customNS).ShouldNot(namespaceFixture.HaveLabel("argocd.argoproj.io/managed-by-cluster-argocd", "openshift-gitops"))

			By("verify 'openshift-gitops_test-1-24-custom' role/rolebinding does not exist in test-1-24-custom")
			openshift_gitops_test_1_24_customRole := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops_test-1-24-custom",
					Namespace: "test-1-24-custom",
				},
			}
			Eventually(openshift_gitops_test_1_24_customRole).Should(k8sFixture.NotExistByName())

			openshift_gitops_test_1_24_customRoleBinding := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops_test-1-24-custom",
					Namespace: "test-1-24-custom",
				},
			}
			Eventually(openshift_gitops_test_1_24_customRoleBinding).Should(k8sFixture.NotExistByName())

			By("3) Delete the 'test-1-24-custom' namespace. In this test, the main reason to do this is to remove the managed-by labels and any other remaining roles/rolebindings")
			Expect(k8sClient.Delete(ctx, test_1_24_customNS)).To(Succeed())

			By("4) Remove source namespace (added in previous steps) from openshift-gitops")

			argocdFixture.Update(openshiftGitOpsArgoCD, func(ac *v1beta1.ArgoCD) {
				ac.Spec.SourceNamespaces = []string{}
			})

			By("verifying Argo CD instance becomes ready")
			Eventually(openshiftGitOpsServer).Should(k8sFixture.ExistByName())
			Eventually(openshiftGitOpsServer).Should(deploymentFixture.HaveReplicas(1))
			Eventually(openshiftGitOpsServer).Should(deploymentFixture.HaveReadyReplicas(1))

			Eventually(openshiftGitOpsAppController).Should(k8sFixture.ExistByName())
			Eventually(openshiftGitOpsAppController).Should(statefulsetFixture.HaveReplicas(1))
			Eventually(openshiftGitOpsAppController).Should(statefulsetFixture.HaveReadyReplicas(1))

			Eventually(test_1_24_customNS).Should(k8sFixture.NotExistByName())

			By("5) create 'test-1-24-custom' namespace again, and add it ArgoCD instance via .spec.sourceNamespaces")

			test_1_24_customNS = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-1-24-custom",
				},
			}
			Expect(k8sClient.Create(ctx, test_1_24_customNS)).To(Succeed())
			argocdFixture.Update(openshiftGitOpsArgoCD, func(ac *v1beta1.ArgoCD) {
				ac.Spec.SourceNamespaces = []string{
					"test-1-24-custom",
				}
			})

			By("verify openshift-gitops workloads become ready")
			Eventually(openshiftGitOpsServer).Should(k8sFixture.ExistByName())
			Eventually(openshiftGitOpsServer).Should(deploymentFixture.HaveReplicas(1))
			Eventually(openshiftGitOpsServer).Should(deploymentFixture.HaveReadyReplicas(1))

			Eventually(openshiftGitOpsAppController).Should(k8sFixture.ExistByName())
			Eventually(openshiftGitOpsAppController).Should(statefulsetFixture.HaveReplicas(1))
			Eventually(openshiftGitOpsAppController).Should(statefulsetFixture.HaveReadyReplicas(1))

			By("verifying test-1-24-custom has managed-by-cluster-argocd label")
			Eventually(test_1_24_customNS, "2m", "5s").Should(namespaceFixture.HaveLabel("argocd.argoproj.io/managed-by-cluster-argocd", "openshift-gitops"))

			By("verify openshift-roles and rolebindings exist. In previous step, they would not exist due to labels on test-1-24-custom. NOW, in this step, they should.")
			openshift_gitops_test_1_24_customRole = &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops_test-1-24-custom",
					Namespace: "test-1-24-custom",
				},
			}
			Eventually(openshift_gitops_test_1_24_customRole).Should(k8sFixture.ExistByName())

			openshift_gitops_test_1_24_customRoleBinding = &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops_test-1-24-custom",
					Namespace: "test-1-24-custom",
				},
			}
			Eventually(openshift_gitops_test_1_24_customRoleBinding).Should(rolebindingFixture.HaveRoleRef(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     "openshift-gitops_test-1-24-custom",
			}))
			Eventually(openshift_gitops_test_1_24_customRoleBinding).Should(rolebindingFixture.HaveSubject(rbacv1.Subject{
				Kind:      "ServiceAccount",
				Name:      "openshift-gitops-argocd-application-controller",
				Namespace: "openshift-gitops",
			}))

			By("6) Add the managed-by label to test-1-24-custom namespace")

			namespaceFixture.Update(test_1_24_customNS, func(n *corev1.Namespace) {
				n.Labels["argocd.argoproj.io/managed-by"] = "openshift-gitops"
			})

			ensureRolesAndRoleBindingsHaveExpectedValuesInTest1_2_24Namespace()

			By("now that the managed-by label has been added, the custom roles should be deleted, and should stay deleted")
			Eventually(openshift_gitops_test_1_24_customRole).ShouldNot(k8sFixture.ExistByName())
			Consistently(openshift_gitops_test_1_24_customRole).ShouldNot(k8sFixture.ExistByName())

			Eventually(openshift_gitops_test_1_24_customRoleBinding).ShouldNot(k8sFixture.ExistByName())
			Consistently(openshift_gitops_test_1_24_customRoleBinding).ShouldNot(k8sFixture.ExistByName())

			Eventually(test_1_24_customNS).ShouldNot(namespaceFixture.HaveLabel("argocd.argoproj.io/managed-by-cluster-argocd", "openshift-gitops"))

			By("7) Remove managed-by from test-1-24-custom and verify the roles exist again")
			namespaceFixture.Update(test_1_24_customNS, func(n *corev1.Namespace) {
				delete(n.Labels, "argocd.argoproj.io/managed-by")
			})

			By("restarts the server and app controller workloads. I presume this is because their startup is too slow to pick up the RBAC changes we have made (removing the label)")
			_, err = osFixture.ExecCommand("oc", "rollout", "restart", "deployment.apps/openshift-gitops-server", "-n", "openshift-gitops")
			Expect(err).ToNot(HaveOccurred())

			_, err = osFixture.ExecCommand("oc", "rollout", "restart", "statefulset.apps/openshift-gitops-application-controller", "-n", "openshift-gitops")
			Expect(err).ToNot(HaveOccurred())

			By("workloads should become available")
			Eventually(openshiftGitOpsServer).Should(k8sFixture.ExistByName())
			Eventually(openshiftGitOpsServer).Should(deploymentFixture.HaveReplicas(1))
			Eventually(openshiftGitOpsServer).Should(deploymentFixture.HaveReadyReplicas(1))

			Eventually(openshiftGitOpsAppController).Should(k8sFixture.ExistByName())
			Eventually(openshiftGitOpsAppController).Should(statefulsetFixture.HaveReplicas(1))
			Eventually(openshiftGitOpsAppController).Should(statefulsetFixture.HaveReadyReplicas(1))

			By("role rolebindings to openshift-gitops instance should exist")
			Eventually(openshift_gitops_test_1_24_customRole).Should(k8sFixture.ExistByName())

			Eventually(openshift_gitops_test_1_24_customRoleBinding).Should(rolebindingFixture.HaveRoleRef(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     "openshift-gitops_test-1-24-custom",
			}))
			Eventually(openshift_gitops_test_1_24_customRoleBinding).Should(rolebindingFixture.HaveSubject(rbacv1.Subject{
				Kind:      "ServiceAccount",
				Name:      "openshift-gitops-argocd-application-controller",
				Namespace: "openshift-gitops",
			}))
			Eventually(openshift_gitops_test_1_24_customRoleBinding).Should(rolebindingFixture.HaveSubject(rbacv1.Subject{
				Kind:      "ServiceAccount",
				Name:      "openshift-gitops-argocd-server",
				Namespace: "openshift-gitops",
			}))

			By("8) Remove namespaces from .spec.sourceNamespaces")
			argocdFixture.Update(openshiftGitOpsArgoCD, func(ac *v1beta1.ArgoCD) {
				ac.Spec.SourceNamespaces = []string{}
			})

			By("verifying managed-by-cluster-argocd label is removed, and the custom role/binding are deleted")
			Eventually(test_1_24_customNS).ShouldNot(namespaceFixture.HaveLabel("argocd.argoproj.io/managed-by-cluster-argocd", "openshift-gitops"))

			Eventually(openshift_gitops_test_1_24_customRole).Should(k8sFixture.NotExistByName())
			Eventually(openshift_gitops_test_1_24_customRoleBinding).Should(k8sFixture.NotExistByName())

		})
	})
})
