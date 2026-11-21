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

		It("verifies that namespaces added to .spec.sourceNamespaces are managed by an Argo CD instance, except when those namespaces also have managed-by label. Both addition and removal of values from this field are tested", func() {

			const argocdName = "argocd-083"

			By("creating a new ArgoCD instance")
			argocdInstance, argocdNS, cleanupArgoCD := fixture.CreateNamespaceWithArgoCDInstance(argocdName)
			defer cleanupArgoCD()

			// sourceNamespaces reconciliation is gated on ARGOCD_CLUSTER_CONFIG_NAMESPACES in argocd-operator
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ARGOCD_CLUSTER_CONFIG_NAMESPACES", "openshift-gitops, "+argocdNS.Name)

			By("enabling ApplicationSet controller on argocd-083 instance")
			argocdFixture.Update(argocdInstance, func(ac *v1beta1.ArgoCD) {
				ac.Spec.ApplicationSet = &v1beta1.ArgoCDApplicationSet{}
			})
			Eventually(argocdInstance, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("1) create test-1-24-custom namespace managed by argocd-083 instance")

			test_1_24_customNS, cleanupFunc := fixture.CreateManagedNamespaceWithCleanupFunc("test-1-24-custom", argocdNS.Name)
			defer cleanupFunc()

			By("verifying argocd-083 workloads exist and are running")

			for _, deploymentToVerify := range []string{
				argocdName + "-redis",
				argocdName + "-repo-server",
				argocdName + "-server",
				argocdName + "-applicationset-controller",
			} {
				depl := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      deploymentToVerify,
						Namespace: argocdNS.Name,
					},
				}
				Eventually(depl).Should(k8sFixture.ExistByName())
				Eventually(depl).Should(deploymentFixture.HaveReplicas(1))
				Eventually(depl, "2m", "5s").Should(deploymentFixture.HaveReadyReplicas(1))
			}

			argocdAppController := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      argocdName + "-application-controller",
					Namespace: argocdNS.Name,
				},
			}
			Eventually(argocdAppController).Should(k8sFixture.ExistByName())

			Eventually(test_1_24_customNS).Should(namespaceFixture.HaveLabel("argocd.argoproj.io/managed-by", argocdNS.Name))

			ensureRolesAndRoleBindingsHaveExpectedValuesInTest1_2_24Namespace := func() {

				By("verifying that " + test_1_24_customNS.Name + " namespace has the expected server/app controller roles/rolebindings, and that the rolebindings grant access to argocd-083 Argo CD instance")

				appControllerRole := &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      argocdName + "-argocd-application-controller",
						Namespace: test_1_24_customNS.Name,
					},
				}
				Eventually(appControllerRole).Should(k8sFixture.ExistByName())

				serverRole := &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      argocdName + "-argocd-server",
						Namespace: test_1_24_customNS.Name,
					},
				}
				Eventually(serverRole).Should(k8sFixture.ExistByName())

				appcontrollerRoleBinding := &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      argocdName + "-argocd-application-controller",
						Namespace: test_1_24_customNS.Name,
					},
				}
				Eventually(appcontrollerRoleBinding).Should(k8sFixture.ExistByName())
				Eventually(appcontrollerRoleBinding).Should(rolebindingFixture.HaveRoleRef(rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "Role",
					Name:     argocdName + "-argocd-application-controller",
				}))
				Eventually(appcontrollerRoleBinding).Should(rolebindingFixture.HaveSubject(rbacv1.Subject{
					Kind:      "ServiceAccount",
					Name:      argocdName + "-argocd-application-controller",
					Namespace: argocdNS.Name,
				}))

				argocdServerRoleBinding := &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      argocdName + "-argocd-server",
						Namespace: test_1_24_customNS.Name,
					},
				}
				Eventually(argocdServerRoleBinding).Should(k8sFixture.ExistByName())
				Eventually(argocdServerRoleBinding).Should(rolebindingFixture.HaveRoleRef(rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "Role",
					Name:     argocdName + "-argocd-server",
				}))
				Eventually(argocdServerRoleBinding).Should(rolebindingFixture.HaveSubject(rbacv1.Subject{
					Kind:      "ServiceAccount",
					Name:      argocdName + "-argocd-server",
					Namespace: argocdNS.Name,
				}))

			}
			ensureRolesAndRoleBindingsHaveExpectedValuesInTest1_2_24Namespace()

			By("2) Adding 'test-1-24-custom' as a source NS to argocd-083 .spec.sourceNamespaces")

			argocdFixture.Update(argocdInstance, func(ac *v1beta1.ArgoCD) {
				ac.Spec.SourceNamespaces = []string{
					"test-1-24-custom",
				}
			})

			By("verifying argocd-083 instance should become ready")
			argocdServer := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      argocdName + "-server",
					Namespace: argocdNS.Name,
				},
			}
			Eventually(argocdServer).Should(k8sFixture.ExistByName())
			Eventually(argocdServer, "3m", "5s").Should(deploymentFixture.HaveReplicas(1))
			Eventually(argocdServer, "3m", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			Eventually(argocdAppController).Should(k8sFixture.ExistByName())
			Eventually(argocdAppController).Should(statefulsetFixture.HaveReplicas(1))
			Eventually(argocdAppController).Should(statefulsetFixture.HaveReadyReplicas(1))

			By("verifing expected managed labels on test-1-24-custom, both managed-by and managed-by-cluster-argocd")
			Eventually(test_1_24_customNS).Should(namespaceFixture.HaveLabel("argocd.argoproj.io/managed-by", argocdNS.Name))

			ensureRolesAndRoleBindingsHaveExpectedValuesInTest1_2_24Namespace()

			Eventually(test_1_24_customNS).ShouldNot(namespaceFixture.HaveLabel("argocd.argoproj.io/managed-by-cluster-argocd", argocdNS.Name))

			sourceNSRoleName := argocdName + "_test-1-24-custom"

			By("verify '" + sourceNSRoleName + "' role/rolebinding does not exist in test-1-24-custom")
			sourceNSRole := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sourceNSRoleName,
					Namespace: "test-1-24-custom",
				},
			}
			Eventually(sourceNSRole).Should(k8sFixture.NotExistByName())

			sourceNSRoleBinding := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sourceNSRoleName,
					Namespace: "test-1-24-custom",
				},
			}
			Eventually(sourceNSRoleBinding).Should(k8sFixture.NotExistByName())

			By("3) Delete the 'test-1-24-custom' namespace. In this test, the main reason to do this is to remove the managed-by labels and any other remaining roles/rolebindings")
			Expect(k8sClient.Delete(ctx, test_1_24_customNS)).To(Succeed())

			By("4) Remove source namespace (added in previous steps) from argocd-083")

			argocdFixture.Update(argocdInstance, func(ac *v1beta1.ArgoCD) {
				ac.Spec.SourceNamespaces = []string{}
			})

			By("verifying Argo CD instance becomes ready")
			Eventually(argocdServer).Should(k8sFixture.ExistByName())
			Eventually(argocdServer).Should(deploymentFixture.HaveReplicas(1))
			Eventually(argocdServer).Should(deploymentFixture.HaveReadyReplicas(1))

			Eventually(argocdAppController).Should(k8sFixture.ExistByName())
			Eventually(argocdAppController).Should(statefulsetFixture.HaveReplicas(1))
			Eventually(argocdAppController).Should(statefulsetFixture.HaveReadyReplicas(1))

			Eventually(test_1_24_customNS).Should(k8sFixture.NotExistByName())

			By("5) create 'test-1-24-custom' namespace again, and add it ArgoCD instance via .spec.sourceNamespaces")

			test_1_24_customNS = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-1-24-custom",
				},
			}
			Expect(k8sClient.Create(ctx, test_1_24_customNS)).To(Succeed())
			argocdFixture.Update(argocdInstance, func(ac *v1beta1.ArgoCD) {
				ac.Spec.SourceNamespaces = []string{
					"test-1-24-custom",
				}
			})

			By("verify argocd-083 workloads become ready")
			Eventually(argocdServer).Should(k8sFixture.ExistByName())
			Eventually(argocdServer).Should(deploymentFixture.HaveReplicas(1))
			Eventually(argocdServer).Should(deploymentFixture.HaveReadyReplicas(1))

			Eventually(argocdAppController).Should(k8sFixture.ExistByName())
			Eventually(argocdAppController).Should(statefulsetFixture.HaveReplicas(1))
			Eventually(argocdAppController).Should(statefulsetFixture.HaveReadyReplicas(1))

			By("verifying test-1-24-custom has managed-by-cluster-argocd label")
			Eventually(test_1_24_customNS, "2m", "5s").Should(namespaceFixture.HaveLabel("argocd.argoproj.io/managed-by-cluster-argocd", argocdNS.Name))

			By("verify roles and rolebindings exist. In previous step, they would not exist due to labels on test-1-24-custom. NOW, in this step, they should.")
			sourceNSRole = &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sourceNSRoleName,
					Namespace: "test-1-24-custom",
				},
			}
			Eventually(sourceNSRole).Should(k8sFixture.ExistByName())

			sourceNSRoleBinding = &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sourceNSRoleName,
					Namespace: "test-1-24-custom",
				},
			}
			Eventually(sourceNSRoleBinding).Should(rolebindingFixture.HaveRoleRef(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     sourceNSRoleName,
			}))
			Eventually(sourceNSRoleBinding).Should(rolebindingFixture.HaveSubject(rbacv1.Subject{
				Kind:      "ServiceAccount",
				Name:      argocdName + "-argocd-application-controller",
				Namespace: argocdNS.Name,
			}))

			By("6) Add the managed-by label to test-1-24-custom namespace")

			namespaceFixture.Update(test_1_24_customNS, func(n *corev1.Namespace) {
				n.Labels["argocd.argoproj.io/managed-by"] = argocdNS.Name
			})

			ensureRolesAndRoleBindingsHaveExpectedValuesInTest1_2_24Namespace()

			By("now that the managed-by label has been added, the custom roles should be deleted, and should stay deleted")
			Eventually(sourceNSRole).ShouldNot(k8sFixture.ExistByName())
			Consistently(sourceNSRole).ShouldNot(k8sFixture.ExistByName())

			Eventually(sourceNSRoleBinding).ShouldNot(k8sFixture.ExistByName())
			Consistently(sourceNSRoleBinding).ShouldNot(k8sFixture.ExistByName())

			Eventually(test_1_24_customNS).ShouldNot(namespaceFixture.HaveLabel("argocd.argoproj.io/managed-by-cluster-argocd", argocdNS.Name))

			By("7) Remove managed-by from test-1-24-custom and verify the roles exist again")
			namespaceFixture.Update(test_1_24_customNS, func(n *corev1.Namespace) {
				delete(n.Labels, "argocd.argoproj.io/managed-by")
			})

			By("restarts the server and app controller workloads. I presume this is because their startup is too slow to pick up the RBAC changes we have made (removing the label)")
			_, err := osFixture.ExecCommand("kubectl", "rollout", "restart", "deployment.apps/"+argocdName+"-server", "-n", argocdNS.Name)
			Expect(err).ToNot(HaveOccurred())

			_, err = osFixture.ExecCommand("kubectl", "rollout", "restart", "statefulset.apps/"+argocdName+"-application-controller", "-n", argocdNS.Name)
			Expect(err).ToNot(HaveOccurred())

			By("workloads should become available")
			Eventually(argocdServer).Should(k8sFixture.ExistByName())
			Eventually(argocdServer).Should(deploymentFixture.HaveReplicas(1))
			Eventually(argocdServer).Should(deploymentFixture.HaveReadyReplicas(1))

			Eventually(argocdAppController).Should(k8sFixture.ExistByName())
			Eventually(argocdAppController).Should(statefulsetFixture.HaveReplicas(1))
			Eventually(argocdAppController).Should(statefulsetFixture.HaveReadyReplicas(1))

			By("role rolebindings to argocd-083 instance should exist")
			Eventually(sourceNSRole).Should(k8sFixture.ExistByName())

			Eventually(sourceNSRoleBinding).Should(rolebindingFixture.HaveRoleRef(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     sourceNSRoleName,
			}))
			Eventually(sourceNSRoleBinding).Should(rolebindingFixture.HaveSubject(rbacv1.Subject{
				Kind:      "ServiceAccount",
				Name:      argocdName + "-argocd-application-controller",
				Namespace: argocdNS.Name,
			}))
			Eventually(sourceNSRoleBinding).Should(rolebindingFixture.HaveSubject(rbacv1.Subject{
				Kind:      "ServiceAccount",
				Name:      argocdName + "-argocd-server",
				Namespace: argocdNS.Name,
			}))

			By("8) Remove namespaces from .spec.sourceNamespaces")
			argocdFixture.Update(argocdInstance, func(ac *v1beta1.ArgoCD) {
				ac.Spec.SourceNamespaces = []string{}
			})

			By("verifying managed-by-cluster-argocd label is removed, and the custom role/binding are deleted")
			Eventually(test_1_24_customNS).ShouldNot(namespaceFixture.HaveLabel("argocd.argoproj.io/managed-by-cluster-argocd", argocdNS.Name))

			Eventually(sourceNSRole).Should(k8sFixture.NotExistByName())
			Eventually(sourceNSRoleBinding).Should(k8sFixture.NotExistByName())

		})
	})
})
