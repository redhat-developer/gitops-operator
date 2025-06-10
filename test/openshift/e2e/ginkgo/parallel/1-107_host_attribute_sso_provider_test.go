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
	routev1 "github.com/openshift/api/route/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-107_host_attribute_sso_provider", func() {

		var (
			k8sClient   client.Client
			ctx         context.Context
			ns          *corev1.Namespace
			cleanupFunc func()
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()

			ns, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

		})

		AfterEach(func() {
			fixture.OutputDebugOnFail(ns)
			if cleanupFunc != nil {
				cleanupFunc()
			}
		})

		It("verifies that keycloak SSO host can be customized", func() {

			By("creating an ArgoCD CR with keycloak enabled and with a custom hsot")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-keycloak", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					SSO: &argov1beta1api.ArgoCDSSOSpec{
						Provider: argov1beta1api.SSOProviderTypeKeycloak,
						Keycloak: &argov1beta1api.ArgoCDKeycloakSpec{
							VerifyTLS: ptr.To(false),
							Host:      "sso.test.example.com",
						},
					},
					Server: argov1beta1api.ArgoCDServerSpec{
						Ingress: argov1beta1api.ArgoCDIngressSpec{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying keycloak route has expected host specified in ArgoCD CR")
			keycloakRoute := &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "keycloak",
					Namespace: ns.Name,
				},
			}
			Eventually(keycloakRoute).Should(k8sFixture.ExistByName())
			Eventually(keycloakRoute.Spec.Host).Should(Equal("sso.test.example.com"))

		})

	})
})
