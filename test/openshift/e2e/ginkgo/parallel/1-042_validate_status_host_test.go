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
	routev1 "github.com/openshift/api/route/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	routeFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/route"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-042_validate_status_host", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that .status.host of ArgoCD matches .spec.host of Route, and status is updated when Route is removed", func() {

			By("creating simple namespace-scoped Argo CD instance")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: ns.Name},
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

			serverRoute := &routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-server", Namespace: ns.Name}}
			Eventually(serverRoute).Should(k8sFixture.ExistByName())

			By("verifying .spec.host of Route equals .status.host of ArgoCD")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(serverRoute), serverRoute); err != nil {
					GinkgoWriter.Println(err)
					return false
				}

				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCD), argoCD); err != nil {
					GinkgoWriter.Println(err)
					return false
				}
				GinkgoWriter.Println("----")
				GinkgoWriter.Println("route URL", serverRoute.Spec.Host)
				GinkgoWriter.Println("status URL", argoCD.Status.Host)

				return serverRoute.Spec.Host == argoCD.Status.Host

			}).Should(BeTrue())

			By("updating host of Route and verifying it is updated in ArgoCD cR")
			routeFixture.Update(serverRoute, func(r *routev1.Route) {
				r.Spec.Host = "modified-route"
			})

			Eventually(argoCD).Should(argocdFixture.HaveHost("modified-route"))

			By("disabling server Route")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Server.Route.Enabled = false
			})

			By("verifying Route host is removed from ArgoCD .status.host")
			Eventually(argoCD).ShouldNot(argocdFixture.HaveHost("modified-route"))
			Consistently(argoCD).ShouldNot(argocdFixture.HaveHost("modified-route"))
			Eventually(argoCD).Should(argocdFixture.HavePhase("Available"))
		})
	})
})
