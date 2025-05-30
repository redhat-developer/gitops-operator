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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	namespaceFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/namespace"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-117_redis_SA_reconciliation", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()

		})

		It("verifies that the Argo CD Redis Deployment has the expected service account name, and if the SA name is modified, it is reverted", func() {

			By("creating basic Argo CD instance and waiting for it to be available")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()
			Eventually(ns).Should(namespaceFixture.HavePhase(corev1.NamespaceActive))

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-example", Namespace: ns.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("waiting for redis deployment to become ready and verifying it has the correct service account name")
			depl := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-example-redis",
					Namespace: ns.Name,
				},
			}
			Eventually(depl).Should(k8sFixture.ExistByName())
			Expect(depl.Spec.Template.Spec.ServiceAccountName).Should(Equal("argocd-example-argocd-redis"))
			Expect(depl.Spec.Template.Spec.DeprecatedServiceAccount).Should(Equal("argocd-example-argocd-redis"))

			Eventually(depl, "2m", "5s").Should(deploymentFixture.HaveAvailableReplicas(1))
			Eventually(depl, "2m", "5s").Should(deploymentFixture.HaveReadyReplicas(1))
			Eventually(depl).Should(deploymentFixture.HaveReplicas(1))
			Eventually(depl).Should(deploymentFixture.HaveUpdatedReplicas(1))

			By("modifying the service account name of the Deployment, simulating a user (or another process besides the opereator) modifying this value")
			deploymentFixture.Update(depl, func(d *appsv1.Deployment) {
				d.Spec.Template.Spec.ServiceAccountName = "argocd-instance-2-argocd-redis"
				d.Spec.Template.Spec.DeprecatedServiceAccount = "argocd-instance-2-argocd-redis"
			})

			By("verifying the operator reverts the service account of the deployment back to the expected value")
			Eventually(depl).Should(deploymentFixture.HaveServiceAccountName("argocd-example-argocd-redis"))
			Consistently(depl).Should(deploymentFixture.HaveServiceAccountName("argocd-example-argocd-redis"))

		})

	})
})
