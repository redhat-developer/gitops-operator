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

	argov1beta1api "github.com/argoproj-labs/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/argoproj-labs/gitops-operator/argocd-operator/common"
	"github.com/argoproj-labs/gitops-operator/argocd-operator/controllers/argocd"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/configmap"
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

		checkClusterConfig := func(argoCDInstance *argov1beta1api.ArgoCD) {
			By("verifying ClusterRole/Bindings exist")
			appController := argocd.GenerateUniqueResourceName(common.ArgoCDApplicationControllerComponent, argoCDInstance)
			appcontrollerCRB := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: appController}}
			Eventually(appcontrollerCRB).Should(k8sFixture.ExistByName())
			appControllerCR := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: appController}}
			Eventually(appControllerCR).Should(k8sFixture.ExistByName())

			server := argocd.GenerateUniqueResourceName(common.ArgoCDServerComponent, argoCDInstance)
			serverCRB := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: server}}
			Eventually(serverCRB).Should(k8sFixture.ExistByName())
			serverCR := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: server}}
			Eventually(serverCR).Should(k8sFixture.ExistByName())

			appSet := argocd.GenerateUniqueResourceName(common.ArgoCDApplicationSetControllerComponent, argoCDInstance)
			appSetClusterRole := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: appSet}}
			Eventually(appSetClusterRole).Should(k8sFixture.ExistByName())
			appSetCRB := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: appSet}}
			Eventually(appSetCRB).Should(k8sFixture.ExistByName())

			By("verifying ConfigMap 'argocd-ssh-known-hosts-cm' has the value from InitialSSHKnownHosts")
			knownHostsCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-ssh-known-hosts-cm",
					Namespace: ns.Name,
				},
			}
			Eventually(knownHostsCM).Should(k8sFixture.ExistByName())
			Eventually(knownHostsCM).Should(configmap.HaveStringDataKeyValue("ssh_known_hosts", "github.com ssh-rsa AAAAB3NzaC1yc2EAAAABIwAAAQEAq2A7hRGmdnm9tUDbO9IDSwBK6TbQa+PXYPCPy6rbTrTtw7PHkccKrpp0yVhp5HdEIcKr6pLlVDBfOLX9QUsyCOV0wzfjIJNlGEYsdlLJizHhbn2mUjvSAHQqZETYP81eFzLQNnPHt4EVVUh7VfDESU84KezmD5QlWpXLmvU31/yMf+Se8xhHTvKSCZIFImWwoG6mbUoWf9nzpIoaSjB+weqqUUmpaaasXVal72J+UX2B+2RPW3RcT0eOzQgqlJL3RKrTJvdsjE3JEAvGq3lGHSZXy28G3skua2SmVi/w4yCE6gbODqnTWlg7+wC604ydGXA8VJiS5ap43JXiUFFAaQ=="))

		}

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

		It("create cluster config (clusterrole, clusterrolebinding, configmap) for namespaces in ARGOCD_CLUSTER_CONFIG_NAMESPACES initially", func() {

			// For local run, this is expected to be set when the operator process is started
			if !fixture.EnvLocalRun() {
				fixture.SetEnvInOperatorSubscriptionOrDeployment("ARGOCD_CLUSTER_CONFIG_NAMESPACES", "openshift-gitops, argocd-e2e-cluster-config")
			}

			By("creating simple cluster-scoped Argo CD instance with initialSSHKnownHosts set")
			ns, cleanupFunc = fixture.CreateNamespaceWithCleanupFunc("argocd-e2e-cluster-config")
			defer cleanupFunc()

			argoCDInstance := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					InitialSSHKnownHosts: argov1beta1api.SSHHostsSpec{
						ExcludeDefaultHosts: true,
						Keys:                "github.com ssh-rsa AAAAB3NzaC1yc2EAAAABIwAAAQEAq2A7hRGmdnm9tUDbO9IDSwBK6TbQa+PXYPCPy6rbTrTtw7PHkccKrpp0yVhp5HdEIcKr6pLlVDBfOLX9QUsyCOV0wzfjIJNlGEYsdlLJizHhbn2mUjvSAHQqZETYP81eFzLQNnPHt4EVVUh7VfDESU84KezmD5QlWpXLmvU31/yMf+Se8xhHTvKSCZIFImWwoG6mbUoWf9nzpIoaSjB+weqqUUmpaaasXVal72J+UX2B+2RPW3RcT0eOzQgqlJL3RKrTJvdsjE3JEAvGq3lGHSZXy28G3skua2SmVi/w4yCE6gbODqnTWlg7+wC604ydGXA8VJiS5ap43JXiUFFAaQ==",
					},
					ApplicationSet: &argov1beta1api.ArgoCDApplicationSet{
						SourceNamespaces: []string{"some-namespace", "some-other-namespace"},
						SCMProviders:     []string{"github.com"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCDInstance)).To(Succeed())

			Eventually(argoCDInstance, "5m", "5s").Should(argocdFixture.BeAvailable())
			Eventually(argoCDInstance, "5m", "5s").Should(argocdFixture.HaveServerStatus("Running"))

			checkClusterConfig(argoCDInstance)
		})

		It("create cluster config (clusterrole, clusterrolebinding, configmap) when namespace added to ARGOCD_CLUSTER_CONFIG_NAMESPACES", func() {

			if fixture.EnvLocalRun() {
				Skip("This test modifies the Subscription/operator deployment env vars, which requires the operator be running on the cluster.")
				return
			}

			// Make sure the namespace is not in ARGOCD_CLUSTER_CONFIG_NAMESPACES initially
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ARGOCD_CLUSTER_CONFIG_NAMESPACES", "openshift-gitops")

			ns, cleanupFunc = fixture.CreateNamespaceWithCleanupFunc("argocd-e2e-cluster-config")

			By("creating simple namespace-scoped ArgoCD instance .spec.initialSSHKnownHosts set")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					InitialSSHKnownHosts: argov1beta1api.SSHHostsSpec{
						ExcludeDefaultHosts: true,
						Keys:                "github.com ssh-rsa AAAAB3NzaC1yc2EAAAABIwAAAQEAq2A7hRGmdnm9tUDbO9IDSwBK6TbQa+PXYPCPy6rbTrTtw7PHkccKrpp0yVhp5HdEIcKr6pLlVDBfOLX9QUsyCOV0wzfjIJNlGEYsdlLJizHhbn2mUjvSAHQqZETYP81eFzLQNnPHt4EVVUh7VfDESU84KezmD5QlWpXLmvU31/yMf+Se8xhHTvKSCZIFImWwoG6mbUoWf9nzpIoaSjB+weqqUUmpaaasXVal72J+UX2B+2RPW3RcT0eOzQgqlJL3RKrTJvdsjE3JEAvGq3lGHSZXy28G3skua2SmVi/w4yCE6gbODqnTWlg7+wC604ydGXA8VJiS5ap43JXiUFFAaQ==",
					},
					ApplicationSet: &argov1beta1api.ArgoCDApplicationSet{
						SourceNamespaces: []string{"some-namespace", "some-other-namespace"},
						SCMProviders:     []string{"github.com"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.HaveServerStatus("Running"))

			By("adding argocd-e2e-cluster-config to ARGOCD_CLUSTER_CONFIG_NAMESPACES")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ARGOCD_CLUSTER_CONFIG_NAMESPACES", "openshift-gitops, argocd-e2e-cluster-config")

			checkClusterConfig(argoCD)
		})
	})
})
