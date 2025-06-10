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
	argocdv1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	appFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	configmapFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/configmap"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	namespaceFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/namespace"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-116_annotation-based-tracking_test", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()

		})

		It("verifies that when annotation tracking is enabled that Argo CD instance starts and has the annotation tracking value specified in ConfigMap", func() {

			By("creating Argo CD instances with annotation+label in two different namespaces")
			nsTestDemo1, cleanupFunc := fixture.CreateNamespaceWithCleanupFunc("argocd-test-demo-1")
			defer cleanupFunc()
			Eventually(nsTestDemo1).Should(namespaceFixture.HavePhase(corev1.NamespaceActive))

			nsTestDemo2, cleanupFunc := fixture.CreateNamespaceWithCleanupFunc("argocd-test-demo-2")
			Eventually(nsTestDemo2).Should(namespaceFixture.HavePhase(corev1.NamespaceActive))
			defer cleanupFunc()

			argoCDTestDemo1 := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-instance-demo-1", Namespace: nsTestDemo1.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					InstallationID:         "instance-demo-1",
					ResourceTrackingMethod: "annotation+label",
				},
			}
			Expect(k8sClient.Create(ctx, argoCDTestDemo1)).To(Succeed())

			argoCDTestDemo2 := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-instance-demo-2", Namespace: nsTestDemo2.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					InstallationID:         "instance-demo-2",
					ResourceTrackingMethod: "annotation+label",
				},
			}
			Expect(k8sClient.Create(ctx, argoCDTestDemo2)).To(Succeed())

			Eventually(argoCDTestDemo1, "5m", "5s").Should(argocdFixture.BeAvailable())
			Eventually(argoCDTestDemo2, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("creating 2 more namespaces, each managed by one of the above Argo CD instances")

			nsAppNS1, cleanupFunc := fixture.CreateManagedNamespaceWithCleanupFunc("app-ns-1", "argocd-test-demo-1")
			defer cleanupFunc()
			Eventually(nsAppNS1).Should(namespaceFixture.HavePhase(corev1.NamespaceActive))

			nsAppNS2, cleanupFunc := fixture.CreateManagedNamespaceWithCleanupFunc("app-ns-2", "argocd-test-demo-2")
			defer cleanupFunc()
			Eventually(nsAppNS2).Should(namespaceFixture.HavePhase(corev1.NamespaceActive))

			By("creating an Application in each Argo CD instance, targeting one of the namespaces and verifying the deploy succeeds")

			appOnTestDemo1 := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "nginx-app", Namespace: argoCDTestDemo1.Namespace},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "test/examples/nginx",
						RepoURL:        "https://github.com/redhat-developer/gitops-operator",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: nsAppNS1.Name,
						Server:    "https://kubernetes.default.svc",
					},
					Project: "default",
					SyncPolicy: &argocdv1alpha1.SyncPolicy{
						Automated: &argocdv1alpha1.SyncPolicyAutomated{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, appOnTestDemo1)).To(Succeed())
			Eventually(appOnTestDemo1, "4m", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(appOnTestDemo1, "4m", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

			appOnTestDemo2 := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "nginx-app", Namespace: argoCDTestDemo2.Namespace},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "test/examples/nginx",
						RepoURL:        "https://github.com/redhat-developer/gitops-operator",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: nsAppNS2.Name,
						Server:    "https://kubernetes.default.svc",
					},
					Project: "default",
					SyncPolicy: &argocdv1alpha1.SyncPolicy{
						Automated: &argocdv1alpha1.SyncPolicyAutomated{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, appOnTestDemo2)).To(Succeed())

			Eventually(appOnTestDemo2, "4m", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(appOnTestDemo2, "4m", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

			Eventually(argoCDTestDemo1, "5m", "5s").Should(argocdFixture.BeAvailable())
			Eventually(argoCDTestDemo2, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying argocd-cm has application.resourceTrackingMethod set to annotation+label, and installationID matches the installationID value from ArgoCD CR, in both Argo CD instances")
			cmInArgoCDTestDemo1 := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-cm",
					Namespace: argoCDTestDemo1.Namespace,
				},
			}
			Eventually(cmInArgoCDTestDemo1).Should(k8sFixture.ExistByName())
			Eventually(cmInArgoCDTestDemo1).Should(configmapFixture.HaveStringDataKeyValue("application.resourceTrackingMethod", "annotation+label"))
			Eventually(cmInArgoCDTestDemo1).Should(configmapFixture.HaveStringDataKeyValue("installationID", "instance-demo-1"))

			cmInArgoCDTestDemo2 := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-cm",
					Namespace: argoCDTestDemo2.Namespace,
				},
			}
			Eventually(cmInArgoCDTestDemo2).Should(k8sFixture.ExistByName())
			Eventually(cmInArgoCDTestDemo2).Should(configmapFixture.HaveStringDataKeyValue("application.resourceTrackingMethod", "annotation+label"))
			Eventually(cmInArgoCDTestDemo2).Should(configmapFixture.HaveStringDataKeyValue("installationID", "instance-demo-2"))

		})

	})
})
