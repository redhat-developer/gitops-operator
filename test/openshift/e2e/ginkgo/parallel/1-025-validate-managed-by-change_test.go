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
	"context"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	argocdv1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	appFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	namespaceFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/namespace"
	rolebindingFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/rolebinding"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-025-validate-managed-by-change", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("ensuring that managed-by label can transition between two different Argo CD instances", func() {

			By("creating 3 namespaces: 2 contain argo cd instances, and one will be managed by one of those namespaces")

			test_1_25_argo1NS, cleanup1 := fixture.CreateNamespaceWithCleanupFunc("test-1-25-argo1")
			defer cleanup1()

			test_1_25_argo2NS, cleanup2 := fixture.CreateNamespaceWithCleanupFunc("test-1-25-argo2")
			defer cleanup2()

			test_1_25_targetNS, cleanup3 := fixture.CreateManagedNamespaceWithCleanupFunc("test-1-25-target", test_1_25_argo1NS.Name)
			defer cleanup3()

			argoCDtest_1_25_argo1 := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: test_1_25_argo1NS.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCDtest_1_25_argo1)).To(Succeed())

			argoCDtest_1_25_argo2 := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: test_1_25_argo2NS.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCDtest_1_25_argo2)).To(Succeed())

			By("waiting for ArgoCD CRs to be reconciled and the instances to be ready")
			Eventually(argoCDtest_1_25_argo1, "3m", "5s").Should(argocdFixture.BeAvailable())
			Eventually(argoCDtest_1_25_argo2, "3m", "5s").Should(argocdFixture.BeAvailable())

			By("ensuring we can deploy to the managed namespace via the ArgoCD instance in first namespace")
			app := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "guestbook", Namespace: argoCDtest_1_25_argo1.Namespace},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "./test/examples/nginx",
						RepoURL:        "https://github.com/jgwest/gitops-operator",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: test_1_25_targetNS.Name,
						Server:    "https://kubernetes.default.svc",
					},
					Project: "default",
					SyncPolicy: &argocdv1alpha1.SyncPolicy{
						Automated: &argocdv1alpha1.SyncPolicyAutomated{},
						Retry:     &argocdv1alpha1.RetryStrategy{Limit: int64(5)},
					},
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			By("waiting for all pods to be ready in both Argo CD namespaces")
			fixture.WaitForAllPodsInTheNamespaceToBeReady(test_1_25_argo1NS.Name, k8sClient)
			fixture.WaitForAllPodsInTheNamespaceToBeReady(test_1_25_argo2NS.Name, k8sClient)

			By("verifying Argo CD Application deployed as expected and is healthy and synced")
			Eventually(app, "3m", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app, "60s", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

			By("update 'test_1_25_target' NS to be managed by the second Argo CD instance, rather than the first")
			namespaceFixture.Update(&test_1_25_targetNS, func(n *corev1.Namespace) {
				n.Labels["argocd.argoproj.io/managed-by"] = "test-1-25-argo2"
			})

			By("verifying that RoleBinding in 'test_1_25_target' is updated to the second namespace")
			roleBindingIntest_1_25_targetNS := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-application-controller", Namespace: test_1_25_targetNS.Name}}
			Eventually(roleBindingIntest_1_25_targetNS).Should(rolebindingFixture.HaveSubject(rbacv1.Subject{
				Kind:      "ServiceAccount",
				Name:      "argocd-argocd-application-controller",
				Namespace: "test-1-25-argo2",
			}))

			By("hard refresh the Application, to pick up changes")
			appFixture.Update(app, func(a *argocdv1alpha1.Application) {
				if a.Annotations == nil {
					a.Annotations = map[string]string{}
				}
				a.Annotations["argocd.argoproj.io/refresh"] = "hard"
			})

			By("creating a new Argo CD Application in second Argo CD namespace")
			app_argo2 := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "guestbook", Namespace: argoCDtest_1_25_argo2.Namespace},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "./test/examples/nginx",
						RepoURL:        "https://github.com/jgwest/gitops-operator",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: test_1_25_targetNS.Name,
						Server:    "https://kubernetes.default.svc",
					},
					Project: "default",
					SyncPolicy: &argocdv1alpha1.SyncPolicy{
						Automated: &argocdv1alpha1.SyncPolicyAutomated{},
						Retry:     &argocdv1alpha1.RetryStrategy{Limit: int64(5)},
					},
				},
			}
			Expect(k8sClient.Create(ctx, app_argo2)).To(Succeed())

			By("First Argo CD instance Application should be unhealthy, because it is no longer managing the namespace")
			Eventually(app, "4m", "1s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusMissing))
			Eventually(app, "4m", "1s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeUnknown))

			By("Second Argo CD instance Application should be healthy, because it is now managing the namespace")
			Eventually(app_argo2, "60s", "1s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app_argo2, "60s", "1s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))
		})

	})
})
