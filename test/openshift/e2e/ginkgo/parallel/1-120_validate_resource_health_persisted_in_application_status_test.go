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
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ArgoCD Resource Health Persist", func() {
	var (
		argoCDName   = "example-argocd"
		appName      = "guestbook"
		appNamespace = "guestbook"
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

			err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      ns.Name,
				Namespace: ns.Namespace,
			}, ns)
			Expect(err).Should(BeNil())

			By("Creating ArgoCD CR with controller.resource.health.persist=true")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Controller: argov1beta1api.ArgoCDApplicationControllerSpec{},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("Waiting for Application Controller to be ready")
			Eventually(func() bool {
				deploy := &appsv1.StatefulSet{}
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      argoCDName + "-application-controller",
					Namespace: ns.Name,
				}, deploy)
				return err == nil && deploy.Status.ReadyReplicas > 0
			}, "1m", "5s").Should(BeTrue())

			targetNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: appNamespace,
					Labels: map[string]string{
						"argocd.argoproj.io/managed-by": ns.Name,
					},
				},
			}
			By("Creating target namespace for Application")
			Expect(k8sClient.Create(ctx, targetNamespace)).To(Succeed())

			By("Creating ArgoCD Application CR")
			app := &argoprojv1a1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      appName,
					Namespace: ns.Name,
				},
				Spec: argoprojv1a1.ApplicationSpec{
					Project: "default",
					Source: &argoprojv1a1.ApplicationSource{
						RepoURL:        "https://github.com/Rizwana777/argocd-example-apps",
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
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      appName,
					Namespace: ns.Name,
				}, &fetched)
				if err != nil {
					return false
				}

				// Check if application has resources with health information
				if len(fetched.Status.Resources) == 0 {
					return false
				}

				for _, res := range fetched.Status.Resources {
					if res.Health == nil {
						return false
					}
					if res.Health.Status == "" {
						return false
					}
				}

				// Validate resourceHealthSource is NOT present (it is omitted when health is persisted)
				return len(fetched.Status.ResourceHealthSource) == 0
			}, "3m", "5s").Should(BeTrue())

			Expect(k8sClient.Delete(ctx, targetNamespace)).To(Succeed())
		})
	})
})
