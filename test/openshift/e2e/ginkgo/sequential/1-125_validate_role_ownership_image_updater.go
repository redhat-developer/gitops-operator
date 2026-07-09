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
	"strings"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	argocdFixture "github.com/argoproj-labs/argocd-operator/tests/ginkgo/fixture/argocd"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	deplFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	osFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/os"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-125_validate_role_ownership_image_updater", func() {

		var (
			ctx         context.Context
			k8sClient   client.Client
			ns          *corev1.Namespace
			cleanupFunc func()
		)
		const (
			imageUpdaterControllerClusterRoleName        = "image-updater-image-updater-argocd-image-updater-controller"
			imageUpdaterControllerClusterRoleBindingName = "image-updater-image-updater-argocd-image-updater-controller"
		)
		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		AfterEach(func() {
			if cleanupFunc != nil {
				cleanupFunc()
			}

			fixture.OutputDebugOnFail(ns)

		})

		It("validates role bug fixes for image updater", func() {
			By("create a simple namespace scoped ArgoCD instance with image updater enabled and watch namespace set to '*'")
			ns, cleanupFunc = fixture.CreateNamespaceWithCleanupFunc("image-updater")

			By("ensuring default service account has anyuid SCC permission")
			serviceAccountUser := "system:serviceaccount:" + ns.Name + ":default"
			output, err := osFixture.ExecCommand("oc", "auth", "can-i", "use", "scc/anyuid", "--as", serviceAccountUser)
			hasPermission := false
			if err == nil && len(output) > 0 {
				// Check if the service account user is already in the users list
				// Remove quotes and whitespace for comparison
				output = strings.TrimSpace(strings.Trim(output, "'\""))
				if strings.Contains(output, serviceAccountUser) {
					hasPermission = true
				}
			}
			if !hasPermission {
				_, err := osFixture.ExecCommand("oc", "adm", "policy", "add-scc-to-user", "anyuid", "-z", "default", "-n", ns.Name)
				Expect(err).NotTo(HaveOccurred(), "Failed to add anyuid SCC to default service account")
			}
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ARGOCD_CLUSTER_CONFIG_NAMESPACES", "openshift-gitops,image-updater")

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "image-updater", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					ImageUpdater: argov1beta1api.ArgoCDImageUpdaterSpec{
						Env: []corev1.EnvVar{
							{
								Name:  "IMAGE_UPDATER_WATCH_NAMESPACES",
								Value: "*",
							},
						},
						Enabled: true},
				},
			}

			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying image updater workload has started argocd-image-updater-controller")
			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "image-updater-argocd-image-updater-controller", Namespace: "image-updater"}}
			Eventually(depl, "2m", "5s").Should(k8sFixture.ExistByName(), "Deployment image-updater-argocd-image-updater-controller did not exist within timeout")
			Eventually(depl, "2m", "5s").Should(deplFixture.HaveReplicas(1), "Deployment image-updater-argocd-image-updater-controller did not have correct replicas within timeout")
			Eventually(depl, "3m", "5s").Should(deplFixture.HaveReadyReplicas(1), "Deployment image-updater-argocd-image-updater-controller was not ready within timeout")

			By("Verify ClusterRole and ClusterRoleBinding for ArgoCD Image Updater Controller")
			clusterRole := &rbacv1.ClusterRole{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: imageUpdaterControllerClusterRoleName}, clusterRole)
			}).Should(Succeed(), "ClusterRole should exist and be fetchable")

			clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: imageUpdaterControllerClusterRoleBindingName}, clusterRoleBinding)
			}).Should(Succeed(), "ClusterRoleBinding should exist and be fetchable")
			initialClusterRoleUid := clusterRole.GetUID()
			initialClusterRoleBindingUid := clusterRoleBinding.GetUID()

			By("Create ArgoCD instance in a new namespace")

			ns1, nsCleanup := fixture.CreateNamespaceWithCleanupFunc("updater")
			defer nsCleanup()

			By("ensuring default service account has anyuid SCC permission")
			serviceAccountUser = "system:serviceaccount:" + ns1.Name + ":default"
			output, err = osFixture.ExecCommand("oc", "auth", "can-i", "use", "scc/anyuid", "--as", serviceAccountUser)
			hasPermission = false
			if err == nil && len(output) > 0 {
				// Check if the service account user is already in the users list
				// Remove quotes and whitespace for comparison
				output = strings.TrimSpace(strings.Trim(output, "'\""))
				if strings.Contains(output, serviceAccountUser) {
					hasPermission = true
				}
			}
			if !hasPermission {
				_, err := osFixture.ExecCommand("oc", "adm", "policy", "add-scc-to-user", "anyuid", "-z", "default", "-n", ns1.Name)
				Expect(err).NotTo(HaveOccurred(), "Failed to add anyuid SCC to default service account")
			}

			argoCD1 := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "image-updater-image",
					Namespace: ns1.Name,
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
			Expect(k8sClient.Create(ctx, argoCD1)).To(Succeed())

			Eventually(argoCD1, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("Verify UID of ClusterRole and ClusterRoleBinding remain the same after creating namespace-scoped ArgoCD instance")
			newClusterRole := &rbacv1.ClusterRole{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: imageUpdaterControllerClusterRoleName}, newClusterRole)
			}).Should(Succeed(), "ClusterRole should exist and be fetchable")
			newClusterRoleUid := newClusterRole.GetUID()

			newClusterRoleBinding := &rbacv1.ClusterRoleBinding{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: imageUpdaterControllerClusterRoleBindingName}, newClusterRoleBinding)
			}).Should(Succeed(), "ClusterRoleBinding should exist and be fetchable")

			newClusterRoleBindingUid := newClusterRoleBinding.GetUID()

			Expect(newClusterRoleUid).To(Equal(initialClusterRoleUid), "ClusterRole UID should remain the same after creating namespace-scoped ArgoCD instance")
			Expect(newClusterRoleBindingUid).To(Equal(initialClusterRoleBindingUid), "ClusterRoleBinding UID should remain the same after creating namespace-scoped ArgoCD instance")
		})
	})
})
