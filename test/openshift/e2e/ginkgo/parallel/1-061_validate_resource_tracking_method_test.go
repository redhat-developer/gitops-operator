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
	configmapFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/configmap"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-061_validate_resource_tracking_method", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies .spec.resourceTrackingMethod can be used to configure Argo CD instance, and the value is set in argocd-cm ConfigMap", func() {

			By("creating simple namespace-scoped Argo CD instance")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
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

			By("verifying ArgoCD CR defaults to resourceTrackingMethod: label")
			argocdConfigMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-cm", Namespace: ns.Name}}
			Eventually(argocdConfigMap).Should(k8sFixture.ExistByName())

			Eventually(argocdConfigMap).Should(configmapFixture.HaveStringDataKeyValue("application.resourceTrackingMethod", "label"))

			By("verifying we can switch ArgoCD CR to resourceTrackingMethod: annotation, and the ConfigMap will be updated")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				argoCD.Spec.ResourceTrackingMethod = "annotation"
			})

			Eventually(argocdConfigMap).Should(configmapFixture.HaveStringDataKeyValue("application.resourceTrackingMethod", "annotation"))

			By("verifying we can switch ArgoCD CR to resourceTrackingMethod: annotation+label, and the ConfigMap will be updated")

			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				argoCD.Spec.ResourceTrackingMethod = "annotation+label"
			})

			Eventually(argocdConfigMap).Should(configmapFixture.HaveStringDataKeyValue("application.resourceTrackingMethod", "annotation+label"))

			By("verifying if an invalid method is specified, then the default is used")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				argoCD.Spec.ResourceTrackingMethod = "invalid_method"
			})

			Eventually(argocdConfigMap).Should(configmapFixture.HaveStringDataKeyValue("application.resourceTrackingMethod", "label"))

		})

	})
})
