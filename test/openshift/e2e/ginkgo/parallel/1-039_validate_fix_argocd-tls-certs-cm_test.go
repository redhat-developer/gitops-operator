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

	Context("1-039_validate_fix_argocd-tls-certs-cm", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that setting an invalid TLS certificate on ArgoCD CR does not replace a valid certificate on TLS ConfigMap", func() {

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

			By("modifying argocd-tls-certs-cm to add valid, empty certificate")

			tlsCertsConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-tls-certs-cm", Namespace: ns.Name},
			}
			Eventually(tlsCertsConfigMap).Should(k8sFixture.ExistByName())

			configmapFixture.Update(tlsCertsConfigMap, func(cm *corev1.ConfigMap) {
				if cm.Data == nil {
					cm.Data = map[string]string{}
				}
				cm.Data["test.example.com"] = "-----BEGIN CERTIFICATE-----  -----END CERTIFICATE-----"
			})

			By("adding invalid certificate to ArgoCD CR")

			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.TLS.InitialCerts = map[string]string{"test.example.com": "BEGIN CERTIFICATE"}
			})

			By("verifying that invalid certificate from ArgoCD never replaces the 'valid' ConfigMap certificate")

			Consistently(func() bool {

				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsCertsConfigMap), tlsCertsConfigMap); err != nil {
					GinkgoWriter.Println(err)
					return true
				}

				if tlsCertsConfigMap.Data == nil {
					return true
				}

				val := tlsCertsConfigMap.Data["test.example.com"]
				GinkgoWriter.Println("ConfigMap value:", val)
				return val != "BEGIN CERTIFICATE"

			}).Should(BeTrue())
		})
	})
})
