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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-079_validate_vars_for_notificaitons", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("ensures that setting an environment variable on notifications controller via ArgoCD CR will cause the env var to be set on notification controller Deployment", func() {
			By("creating an Argo CD instance with notification enabled")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-argocd",
					Namespace: ns.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{
					Notifications: argov1beta1api.ArgoCDNotifications{
						Enabled: true,
					},
					Server: argov1beta1api.ArgoCDServerSpec{
						Route: argov1beta1api.ArgoCDRouteSpec{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.HaveNotificationControllerStatus("Running"))

			By("waiting for notification controller to be ready")
			notificationsController := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-notifications-controller", Namespace: ns.Name}}
			Eventually(notificationsController).Should(k8sFixture.ExistByName())
			Eventually(notificationsController).Should(deploymentFixture.HaveReadyReplicas(1))

			By("adding environment variable to Notifications controller via ArgoCD CR")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Notifications.Enabled = true
				ac.Spec.Notifications.Env = []corev1.EnvVar{{Name: "foo", Value: "bar"}}
			})

			By("verifying env var is set on the notification controller Deployment, and the Deployment becomes ready")
			Eventually(notificationsController).Should(deploymentFixture.HaveContainerWithEnvVar("foo", "bar", 0))
			Eventually(notificationsController).Should(deploymentFixture.HaveReadyReplicas(1))

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.HaveNotificationControllerStatus("Running"))
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.HaveServerStatus("Running"))

		})

	})
})
