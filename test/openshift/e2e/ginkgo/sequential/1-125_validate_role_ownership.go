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
	deplFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

		It("validates that the role bug is fixed", func() {

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

			Eventually(defaultArgocd, "5m", "5s").Should(argocdFixture.BeAvailable())
			argocdFixture.Update(defaultArgocd, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ImageUpdater = argov1beta1api.ArgoCDImageUpdaterSpec{
					Env: []corev1.EnvVar{
						{
							Name:  "IMAGE_UPDATER_WATCH_NAMESPACES",
							Value: "*",
						},
					},
					Enabled: true,
				}
			})
			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(defaultArgocd, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying image updater workload has started argocd-image-updater-controller")
			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-argocd-image-updater-controller", Namespace: "openshift-gitops"}}
			Eventually(depl, "2m", "5s").Should(k8sFixture.ExistByName(), "Deployment openshift-gitops-argocd-image-updater-controller did not exist within timeout")
			Eventually(depl, "2m", "5s").Should(deplFixture.HaveReplicas(1), "Deployment openshift-gitops-argocd-image-updater-controller did not have correct replicas within timeout")
			Eventually(depl, "3m", "5s").Should(deplFixture.HaveReadyReplicas(1), "Deployment openshift-gitops-argocd-image-updater-controller was not ready within timeout")

			defaultImageUpdaterClusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "openshift-gitops-openshift-gitops-argocd-image-updater-controller",
				},
			}
			defaultImageUpdaterClusterRoleBinding := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "openshift-gitops-openshift-gitops-argocd-image-updater-controller",
				},
			}
			Eventually(defaultImageUpdaterClusterRole).Should(k8sFixture.ExistByName())
			Eventually(defaultImageUpdaterClusterRoleBinding).Should(k8sFixture.ExistByName())

			initialImageUpdaterClusterRoleUid := defaultImageUpdaterClusterRole.GetUID()
			inititalImageUpdaterClusterRoleBindingUid := defaultImageUpdaterClusterRoleBinding.GetUID()

			By("creating new ArgoCD instance to trigger the check")
			ns, nsCleanup := fixture.CreateNamespaceWithCleanupFunc("gitops")
			defer nsCleanup()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-openshift",
					Namespace: ns.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{
					ImageUpdater: argov1beta1api.ArgoCDImageUpdaterSpec{
						Env: []corev1.EnvVar{
							{
								Name:  "IMAGE_UPDATER_LOGLEVEL",
								Value: "trace",
							},
						},
						Enabled: true,
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("checking that the default ClusterRole for the ArgoCD Application Controller still exists")
			newControllerClusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: applicationControllerClusterRoleName,
				},
			}
			newApplicationSetControllerClusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: applicationSetControllerClusterRoleName,
				},
			}
			newServerClusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: serverClusterRoleName,
				},
			}
			newControllerClusterRoleBinding := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: applicationControllerClusterRoleBindingName,
				},
			}
			newApplicationSetControllerClusterRoleBinding := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: applicationSetControllerClusterRoleBindingName,
				},
			}
			newServerClusterRoleBinding := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: serverClusterRoleBindingName,
				},
			}
			newImageUpdaterClusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "openshift-gitops-openshift-gitops-argocd-image-updater-controller",
				},
			}
			newImageUpdaterClusterRoleBinding := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "openshift-gitops-openshift-gitops-argocd-image-updater-controller",
				},
			}

			Eventually(newControllerClusterRole).Should(k8sFixture.ExistByName())
			Eventually(newApplicationSetControllerClusterRole).Should(k8sFixture.ExistByName())
			Eventually(newServerClusterRole).Should(k8sFixture.ExistByName())
			Eventually(newImageUpdaterClusterRole).Should(k8sFixture.ExistByName())

			Eventually(newControllerClusterRoleBinding).Should(k8sFixture.ExistByName())
			Eventually(newApplicationSetControllerClusterRoleBinding).Should(k8sFixture.ExistByName())
			Eventually(newServerClusterRoleBinding).Should(k8sFixture.ExistByName())
			Eventually(newImageUpdaterClusterRoleBinding).Should(k8sFixture.ExistByName())

			By("fetching UID of the clusterrole after reconciliation")
			afterControllerReconcileUid := newControllerClusterRole.GetUID()
			afterApplicationSetControllerReconcileUid := newApplicationSetControllerClusterRole.GetUID()
			afterServerReconcileUid := newServerClusterRole.GetUID()
			afterImageUpdaterReconcileUid := newImageUpdaterClusterRole.GetUID()

			afterControllerRoleBindingReconcileUid := newControllerClusterRoleBinding.GetUID()
			afterApplicationSetControllerRoleBindingReconcileUid := newApplicationSetControllerClusterRoleBinding.GetUID()
			afterServerRoleBindingReconcileUid := newServerClusterRoleBinding.GetUID()
			afterImageUpdaterRoleBindingReconcileUid := newImageUpdaterClusterRoleBinding.GetUID()

			By("comparing the UID to check if the ClusterRole was recreated")
			Expect(initialControllerUid).To(Equal(afterControllerReconcileUid), "the ClusterRole was recreated")
			Expect(initialApplicationSetControllerUid).To(Equal(afterApplicationSetControllerReconcileUid), "the ClusterRole was recreated")
			Expect(initialServerUid).To(Equal(afterServerReconcileUid), "the ClusterRole was recreated")
			Expect(initialImageUpdaterClusterRoleUid).To(Equal(afterImageUpdaterReconcileUid), "the ClusterRole was recreated")

			Expect(initialControllerRoleBindingUid).To(Equal(afterControllerRoleBindingReconcileUid), "the ClusterRoleBinding was recreated")
			Expect(initialApplicationSetControllerRoleBindingUid).To(Equal(afterApplicationSetControllerRoleBindingReconcileUid), "the ClusterRoleBinding was recreated")
			Expect(initialServerRoleBindingUid).To(Equal(afterServerRoleBindingReconcileUid), "the ClusterRoleBinding was recreated")
			Expect(inititalImageUpdaterClusterRoleBindingUid).To(Equal(afterImageUpdaterRoleBindingReconcileUid), "the ClusterRoleBinding was recreated")

		})

	})
})
