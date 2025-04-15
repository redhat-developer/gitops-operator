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
	"crypto/tls"
	"io"
	"net/http"
	"strings"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	routeFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/route"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-030_validate_reencrypt", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies Argo CD Server's Route can be enabled with TLSTerminationReencrypt", func() {

			By("creating namespace-scoped Argo CD instance with rencrypt Route")

			test_1_30_argo1, cleanupFunc := fixture.CreateNamespaceWithCleanupFunc("test-1-30-argo1")
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: test_1_30_argo1.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Server: argov1beta1api.ArgoCDServerSpec{
						Route: argov1beta1api.ArgoCDRouteSpec{
							Enabled: true,
							TLS: &routev1.TLSConfig{
								Termination:                   routev1.TLSTerminationReencrypt,
								InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying Argo CD Server's Route has the expected values")
			r := &routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "argocd-server", Namespace: test_1_30_argo1.Name}}
			Eventually(r).Should(k8sFixture.ExistByName())

			Expect(r).Should(routeFixture.HavePort(intstr.FromString("https")))
			Expect(r).Should(routeFixture.HaveTLS(routev1.TLSTerminationReencrypt, routev1.InsecureEdgeTerminationPolicyRedirect))
			Expect(r).Should(routeFixture.HaveTo(routev1.RouteTargetReference{Kind: "Service", Name: "argocd-server", Weight: ptr.To(int32(100))}))

			By("verifying the Route was successfully admitted, and ths TLS Secret exists")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(r), r); err != nil {
					GinkgoWriter.Println(err)
					return false
				}

				ingressSlice := r.Status.Ingress
				if ingressSlice == nil || len(ingressSlice) != 1 {
					return false
				}

				ingress := ingressSlice[0]

				if ingress.Conditions == nil || len(ingress.Conditions) != 1 {
					return false
				}

				condition := ingress.Conditions[0]

				return condition.Status == "True" && condition.Type == routev1.RouteAdmitted

			}).Should(BeTrue(), ".status.ingress.conditions[0] should have status:true and type:admitted")

			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "argocd-server-tls", Namespace: test_1_30_argo1.Name}}
			Eventually(secret).Should(k8sFixture.ExistByName())

			fixture.WaitForAllPodsInTheNamespaceToBeReady(test_1_30_argo1.Name, k8sClient)

			By("verify we can access the route, and it contains a specific expected string")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(r), r); err != nil {
					GinkgoWriter.Println("Error on retrieving route:", err)
					return false
				}

				if len(r.Status.Ingress) == 0 {
					return false
				}

				host := r.Status.Ingress[0].Host

				// Create a custom HTTP transport
				tr := &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true, // Disable TLS certificate verification
					},
				}

				// Create an HTTP client with the custom transport
				client := &http.Client{Transport: tr}

				// Make a GET request
				resp, err := client.Get("https://" + host)
				if err != nil {
					GinkgoWriter.Println("Error:", err)
					return false
				}
				defer resp.Body.Close()

				// Read the response body
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					GinkgoWriter.Println("Error reading body:", err)
					return false
				}

				// Print the response body
				GinkgoWriter.Println(string(body))

				return strings.Contains(string(body), "Your browser does not support JavaScript.")

			}, "90s", "5s").Should(BeTrue())

		})

	})
})
