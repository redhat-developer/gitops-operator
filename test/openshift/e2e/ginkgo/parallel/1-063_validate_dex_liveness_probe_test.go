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
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-063_validate_dex_liveness_probe_test", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies dex server has expected liveness probe values", func() {

			By("creating an Argo CD instance with Dex SSO enabled")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd",
					Namespace: ns.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{
					SSO: &argov1beta1api.ArgoCDSSOSpec{
						Provider: argov1beta1api.SSOProviderTypeDex,
						Dex: &argov1beta1api.ArgoCDDexSpec{
							Config: "test-config",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying dex-server Deployment has expected liveness probe values")
			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: argoCD.Name + "-dex-server", Namespace: ns.Name}}
			Eventually(depl).Should(k8sFixture.ExistByName())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(depl), depl)).To(Succeed())
				g.Expect(depl.Spec.Template.Spec.Containers).To(HaveLen(1))

				livenessProbe := depl.Spec.Template.Spec.Containers[0].LivenessProbe
				g.Expect(livenessProbe).ToNot(BeNil())
				g.Expect(livenessProbe.FailureThreshold).To(Equal(int32(3)))
				g.Expect(livenessProbe.HTTPGet).ToNot(BeNil())
				g.Expect(livenessProbe.HTTPGet.Path).To(Equal("/healthz/live"))
				g.Expect(livenessProbe.HTTPGet.Port).To(Equal(intstr.FromInt(5558)))
				g.Expect(livenessProbe.HTTPGet.Scheme).To(Equal(corev1.URISchemeHTTP))
				g.Expect(livenessProbe.InitialDelaySeconds).To(Equal(int32(60)))
				g.Expect(livenessProbe.PeriodSeconds).To(Equal(int32(30)))
				g.Expect(livenessProbe.SuccessThreshold).To(Equal(int32(1)))
				g.Expect(livenessProbe.TimeoutSeconds).To(Equal(int32(1)))
			}).Should(Succeed())
		})

	})
})
