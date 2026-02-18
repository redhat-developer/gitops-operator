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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	applicationFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-122_validate_user_defined_appproject", func() {

		var (
			k8sClient    client.Client
			ctx          context.Context
			ns           *corev1.Namespace
			cleanupFunc  func()
			cleanupFuncs []func()
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		AfterEach(func() {
			// Clean up all additional namespaces
			for _, cleanup := range cleanupFuncs {
				if cleanup != nil {
					cleanup()
				}
			}
			cleanupFuncs = nil

			if cleanupFunc != nil {
				cleanupFunc()
			}

			fixture.OutputDebugOnFail(ns)
		})

		It("verifies creating and configuring a user-defined AppProject instance with target namespaces and Application CR referencing it", func() {

			By("creating namespace-scoped Argo CD instance")
			ns, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "8m", "10s").Should(argocdFixture.BeAvailable())

			By("creating target namespace for deployment")
			var targetNS *corev1.Namespace
			targetNS, targetCleanup := fixture.CreateManagedNamespaceWithCleanupFunc(ns.Name+"-target", ns.Name)
			cleanupFuncs = append(cleanupFuncs, targetCleanup)

			By("creating and configuring a user-defined AppProject instance with the target namespace")
			appProjectName := "user-defined-project"
			appProject := &argocdv1alpha1.AppProject{
				ObjectMeta: metav1.ObjectMeta{
					Name:      appProjectName,
					Namespace: ns.Name,
				},
				Spec: argocdv1alpha1.AppProjectSpec{
					SourceRepos: []string{"*"},
					Destinations: []argocdv1alpha1.ApplicationDestination{
						{
							Server:    "https://kubernetes.default.svc",
							Namespace: targetNS.Name,
						},
					},
					ClusterResourceWhitelist: []argocdv1alpha1.ClusterResourceRestrictionItem{{
						Group: "*",
						Kind:  "*",
					}},
				},
			}
			Expect(k8sClient.Create(ctx, appProject)).To(Succeed())

			By("verifying AppProject exists and is configured correctly")
			Eventually(appProject, "2m", "5s").Should(k8sFixture.ExistByName(), "AppProject did not exist within timeout")

			// Verify AppProject configuration
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(appProject), appProject)).To(Succeed())
			Expect(len(appProject.Spec.Destinations)).To(BeNumerically(">", 0), "AppProject should have at least one destination configured")
			Expect(appProject.Spec.Destinations[0].Namespace).To(Equal(targetNS.Name), "AppProject destination should match target namespace")
			Expect(appProject.Spec.Destinations[0].Server).To(Equal("https://kubernetes.default.svc"), "AppProject destination server should be configured")

			By("creating and configuring the Application CR to reference the target namespace and user-defined AppProject instance")
			app := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-app",
					Namespace: ns.Name,
				},
				Spec: argocdv1alpha1.ApplicationSpec{
					Project: appProjectName,
					Source: &argocdv1alpha1.ApplicationSource{
						RepoURL:        "https://github.com/redhat-developer/gitops-operator",
						Path:           "test/examples/nginx",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Server:    "https://kubernetes.default.svc",
						Namespace: targetNS.Name,
					},
					SyncPolicy: &argocdv1alpha1.SyncPolicy{
						Automated: &argocdv1alpha1.SyncPolicyAutomated{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			By("verifying Application is healthy and syncs successfully")
			Eventually(app, "8m", "10s").Should(applicationFixture.HaveHealthStatusCode(health.HealthStatusHealthy), "Application did not reach healthy status within timeout")
			Eventually(app, "8m", "10s").Should(applicationFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced), "Application did not sync within timeout")

			By("verifying Application references the user-defined AppProject")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(app), app)).To(Succeed())
			Expect(app.Spec.Project).To(Equal(appProjectName), "Application should reference the user-defined AppProject")

			By("verifying Application targets the correct namespace")
			Expect(app.Spec.Destination.Namespace).To(Equal(targetNS.Name), "Application should target the configured namespace")

		})
	})
})
