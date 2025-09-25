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
	argoprojv1a1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	ssFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Test", func() {
	const (
		argoCDName   = "example-argocd"
		appName      = "guestbook"
		appNamespace = "guestbook-1-120"
	)

	var (
		k8sClient client.Client
		ctx       context.Context
	)

	BeforeEach(func() {
		fixture.EnsureParallelCleanSlate()

		k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
		ctx = context.Background()

	})

	Context("1-120_validate_resource_health_persisted_in_application_status", func() {
		It("should persist resource health in Application CR status when configured", func() {
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name:      ns.Name,
				Namespace: ns.Namespace,
			}, ns)).To(Succeed())

			By("Creating ArgoCD CR with controller.resource.health.persist=true, which is the default")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					CmdParams: map[string]string{
						"controller.resource.health.persist": "true",
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("Waiting for Application Controller to be ready")
			ss := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
				Name:      argoCDName + "-application-controller",
				Namespace: ns.Name,
			}}
			Eventually(ss).Should(ssFixture.HaveReadyReplicas(1))

			targetNamespace, cleanupFunc := fixture.CreateManagedNamespaceWithCleanupFunc(appNamespace, ns.Name)
			defer cleanupFunc()

			By("Creating ArgoCD Application CR")
			app := &argoprojv1a1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      appName,
					Namespace: ns.Name,
				},
				Spec: argoprojv1a1.ApplicationSpec{
					Project: "default",
					Source: &argoprojv1a1.ApplicationSource{
						RepoURL:        "https://github.com/argoproj/argocd-example-apps",
						Path:           "guestbook",
						TargetRevision: "HEAD",
					},
					Destination: argoprojv1a1.ApplicationDestination{
						Server:    "https://kubernetes.default.svc",
						Namespace: targetNamespace.Name,
					},
					SyncPolicy: &argoprojv1a1.SyncPolicy{
						Automated: &argoprojv1a1.SyncPolicyAutomated{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			By("Validating that resource health is persisted in Application CR")
			Eventually(func() bool {
				var fetched argoprojv1a1.Application
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: ns.Name}, &fetched); err != nil {
					GinkgoWriter.Println("failed to fetch Application:", err)
					return false
				}

				// Check if application has resources with health information
				if len(fetched.Status.Resources) == 0 {
					GinkgoWriter.Println("Application.Status.Resources is empty")
					return false
				}

				for _, res := range fetched.Status.Resources {
					if res.Health == nil {
						GinkgoWriter.Println("Resource", res.Kind, res.Name, "has nil Health")
						return false
					}
					if res.Health.Status == "" {
						GinkgoWriter.Println("Resource", res.Kind, res.Name, "has empty Health.Status")
						return false
					}
				}

				// Validate resourceHealthSource is NOT present (it is omitted when health is persisted)
				return len(fetched.Status.ResourceHealthSource) == 0
			}, "3m", "5s").Should(BeTrue())
		})

		It("should not persist resource health and use resourceHealthSource when controller.resource.health.persist=false", func() {
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			By("Creating ArgoCD CR with controller.resource.health.persist=false")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: argoCDName, Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					CmdParams: map[string]string{
						"controller.resource.health.persist": "false",
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			ss := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
				Name:      argoCDName + "-application-controller",
				Namespace: ns.Name,
			}}
			Eventually(ss).Should(ssFixture.HaveReadyReplicas(1))

			targetNamespace, cleanupFunc := fixture.CreateManagedNamespaceWithCleanupFunc(appNamespace, ns.Name)
			defer cleanupFunc()

			By("Creating ArgoCD Application CR")
			app := &argoprojv1a1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: ns.Name},
				Spec: argoprojv1a1.ApplicationSpec{
					Project: "default",
					Source: &argoprojv1a1.ApplicationSource{
						RepoURL:        "https://github.com/argoproj/argocd-example-apps",
						Path:           "guestbook",
						TargetRevision: "HEAD",
					},
					Destination: argoprojv1a1.ApplicationDestination{
						Server:    "https://kubernetes.default.svc",
						Namespace: targetNamespace.Name,
					},
					SyncPolicy: &argoprojv1a1.SyncPolicy{
						Automated: &argoprojv1a1.SyncPolicyAutomated{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			By("Validating that health is NOT persisted and resourceHealthSource is appTree")
			Eventually(func() bool {
				var fetched argoprojv1a1.Application
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: ns.Name}, &fetched); err != nil {
					GinkgoWriter.Println("failed to fetch Application:", err)
					return false
				}

				// Expect resourceHealthSource to be set
				if fetched.Status.ResourceHealthSource != "appTree" {
					GinkgoWriter.Println("ResourceHealthSource is not set as expected")
					return false
				}

				// Ensure resources exist but Health is not populated
				for _, res := range fetched.Status.Resources {
					if res.Health != nil {
						GinkgoWriter.Println("Expected nil Health but got:", res.Kind, res.Name, "Health:", res.Health.Status)
						return false
					}
				}
				return true
			}, "3m", "5s").Should(BeTrue())
		})
	})
})
