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
	configmapFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/configmap"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"

	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-003_validate_cluster_config", func() {

		var (
			ctx         context.Context
			k8sClient   client.Client
			ns          *corev1.Namespace
			cleanupFunc func()
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		AfterEach(func() {

			fixture.OutputDebugOnFail("argocd-e2e-cluster-config", "openshift-gitops")

			fixture.RestoreSubcriptionToDefault() // revert Subscription at end of test

			if cleanupFunc != nil {
				cleanupFunc()
			}

		})

		It("verifies that adding namespaces to ARGOCD_CLUSTER_CONFIG_NAMESPACES will cause clusterrole and clusterrolebinding to be created for server, app controller, and application set", func() {

			if fixture.EnvLocalRun() {
				Skip("This test modifies the Subscription/operator deployment env vars, which requires the operator be running on the cluster.")
				return
			}

			ns, cleanupFunc = fixture.CreateNamespaceWithCleanupFunc("argocd-e2e-cluster-config")

			By("creating simple namespace-scoped ArgoCD instance .spec.initialSSHKnownHosts set")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					InitialSSHKnownHosts: argov1beta1api.SSHHostsSpec{
						ExcludeDefaultHosts: true,
						Keys:                "github.com ssh-rsa AAAAB3NzaC1yc2EAAAABIwAAAQEAq2A7hRGmdnm9tUDbO9IDSwBK6TbQa+PXYPCPy6rbTrTtw7PHkccKrpp0yVhp5HdEIcKr6pLlVDBfOLX9QUsyCOV0wzfjIJNlGEYsdlLJizHhbn2mUjvSAHQqZETYP81eFzLQNnPHt4EVVUh7VfDESU84KezmD5QlWpXLmvU31/yMf+Se8xhHTvKSCZIFImWwoG6mbUoWf9nzpIoaSjB+weqqUUmpaaasXVal72J+UX2B+2RPW3RcT0eOzQgqlJL3RKrTJvdsjE3JEAvGq3lGHSZXy28G3skua2SmVi/w4yCE6gbODqnTWlg7+wC604ydGXA8VJiS5ap43JXiUFFAaQ==",
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("adding argocd-e2e-cluster-config to ARGOCD_CLUSTER_CONFIG_NAMESPACES")

			fixture.SetEnvInOperatorSubscriptionOrDeployment("ARGOCD_CLUSTER_CONFIG_NAMESPACES", "openshift-gitops, argocd-e2e-cluster-config")

			By("verifying ClusterRole/Binding were created for argocd-e2e-cluster-config server/app controller components, now that the namespace is specified in the CLUSTER_CONFIG env var")
			appControllerCR := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-argocd-e2e-cluster-config-argocd-application-controller"}}
			Eventually(appControllerCR, "2m", "5s").Should(k8sFixture.ExistByName())

			appControllerCRB := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-argocd-e2e-cluster-config-argocd-application-controller"}}
			Eventually(appControllerCRB).Should(k8sFixture.ExistByName())

			serverCR := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-argocd-e2e-cluster-config-argocd-server"}}
			Eventually(serverCR).Should(k8sFixture.ExistByName())

			serverCRB := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-argocd-e2e-cluster-config-argocd-server"}}
			Eventually(serverCRB).Should(k8sFixture.ExistByName())

			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())
			Eventually(argoCD).Should(argocdFixture.HaveServerStatus("Running"))

			By("verifying that the initialSSHKnownHosts value was set in the ConfigMap")

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-ssh-known-hosts-cm",
					Namespace: "argocd-e2e-cluster-config",
				},
			}
			Eventually(cm).Should(k8sFixture.ExistByName())
			Eventually(cm).Should(configmapFixture.HaveStringDataKeyValue("ssh_known_hosts", "github.com ssh-rsa AAAAB3NzaC1yc2EAAAABIwAAAQEAq2A7hRGmdnm9tUDbO9IDSwBK6TbQa+PXYPCPy6rbTrTtw7PHkccKrpp0yVhp5HdEIcKr6pLlVDBfOLX9QUsyCOV0wzfjIJNlGEYsdlLJizHhbn2mUjvSAHQqZETYP81eFzLQNnPHt4EVVUh7VfDESU84KezmD5QlWpXLmvU31/yMf+Se8xhHTvKSCZIFImWwoG6mbUoWf9nzpIoaSjB+weqqUUmpaaasXVal72J+UX2B+2RPW3RcT0eOzQgqlJL3RKrTJvdsjE3JEAvGq3lGHSZXy28G3skua2SmVi/w4yCE6gbODqnTWlg7+wC604ydGXA8VJiS5ap43JXiUFFAaQ=="))

			By("adding source namespaces and additional SCM providers to applications to controller")

			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ApplicationSet = &argov1beta1api.ArgoCDApplicationSet{
					SourceNamespaces: []string{
						"some-namespace",
						"some-other-namespace",
					},
					SCMProviders: []string{
						"github.com",
					},
				}
			})

			By("verifying applicationset controller becomes available")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())
			Eventually(argoCD).Should(argocdFixture.HaveApplicationSetControllerStatus("Running"))

			By("verifying ClusterRole/RoleBinding were created for the argocd-e2e-cluster-config namespace")
			appsetControllerCR := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-argocd-e2e-cluster-config-argocd-applicationset-controller"}}
			Eventually(appsetControllerCR, "2m", "5s").Should(k8sFixture.ExistByName())

			appsetControllerCRB := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-argocd-e2e-cluster-config-argocd-applicationset-controller"}}
			Eventually(appsetControllerCRB).Should(k8sFixture.ExistByName())

		})

	})
})
