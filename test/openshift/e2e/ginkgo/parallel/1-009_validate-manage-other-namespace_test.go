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

package parallel

import (
	"context"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	argocdv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	appFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/namespace"
	secretFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/secret"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-009_validate-manage-other-namespace", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that a namespace-scoped Argo CD in one namespace is able to manage another namespace via the managed-by label", func() {

			nsTest_1_9_custom, cleanupFunc1 := fixture.CreateNamespaceWithCleanupFunc("test-1-9-custom")
			defer cleanupFunc1()

			randomNS, cleanupFunc2 := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc2()

			By("creating simple namespace-scoped Argo CD")
			argoCDInRandomNS := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: randomNS.Name},
			}
			Expect(k8sClient.Create(ctx, argoCDInRandomNS)).To(Succeed())

			By("waiting for Argo CD to be available")
			Eventually(argoCDInRandomNS, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("modifying the labels of another namespace to add the argocd managed-by label")
			namespace.Update(nsTest_1_9_custom, func(n *corev1.Namespace) {
				n.ObjectMeta.Labels["argocd.argoproj.io/managed-by"] = argoCDInRandomNS.Namespace
			})

			By("verifying that Argo CD eventually includes this other namespace in its Secret list of managed namespaces")
			defaultClusterConfigSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-default-cluster-config",
					Namespace: argoCDInRandomNS.Namespace,
				},
			}
			Eventually(defaultClusterConfigSecret, "90s", "5s").Should(k8sFixture.ExistByName())

			Eventually(defaultClusterConfigSecret).Should(
				secretFixture.HaveStringDataKeyValue("namespaces", argoCDInRandomNS.Namespace+","+nsTest_1_9_custom.Name))

			By("creating Argo CD Application targeting the other namespace")
			app := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "test-1-9-custom", Namespace: argoCDInRandomNS.Namespace},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "test/examples/nginx",
						RepoURL:        "https://github.com/redhat-developer/gitops-operator",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: nsTest_1_9_custom.Name,
						Server:    "https://kubernetes.default.svc",
					},
					Project: "default",
					SyncPolicy: &argocdv1alpha1.SyncPolicy{
						Automated: &argocdv1alpha1.SyncPolicyAutomated{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			By("verifying that Argo CD is able to deploy to that other namespace")
			Eventually(app, "4m", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app, "4m", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

			By("removing managed-by label from that other Namespace")
			namespace.Update(nsTest_1_9_custom, func(n *corev1.Namespace) {
				delete(n.ObjectMeta.Labels, "argocd.argoproj.io/managed-by")
			})

			By("verifying label is removed from Argo CD Secret")
			Eventually(defaultClusterConfigSecret).Should(
				secretFixture.HaveStringDataKeyValue("namespaces", argoCDInRandomNS.Namespace))

			By("verifying Argo CD managed-by roles and rolebindings are removed from other namespace")
			rolesToCheck := []string{"argocd-argocd-server", "argocd-argocd-application-controller", "argocd-argocd-redis-ha"}

			for _, roleToCheck := range rolesToCheck {
				role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: roleToCheck, Namespace: nsTest_1_9_custom.Name}}
				Eventually(role).Should(k8sFixture.NotExistByName())
				Consistently(role).Should(k8sFixture.NotExistByName())
			}

			rbToCheck := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-server", Namespace: nsTest_1_9_custom.Name}}
			Eventually(rbToCheck).Should(k8sFixture.NotExistByName())
			Consistently(rbToCheck).Should(k8sFixture.NotExistByName())
		})

	})
})
