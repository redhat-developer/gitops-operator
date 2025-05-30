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

	argov1alpha1api "github.com/argoproj-labs/argocd-operator/api/v1alpha1"
	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-055_drop_resource_customizations", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that resource customization is dropped in conversion from ArgoCD v1alpha1 to v1beta1", func() {

			By("creating simple namespace-scoped Argo CD instance via v1alpha1 API, with resourcecustomization and resourcehealthcheck")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			alphaArgoCD := &argov1alpha1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd",
					Namespace: ns.Name,
				},
				Spec: argov1alpha1api.ArgoCDSpec{
					ResourceCustomizations: `PersistentVolumeClaim:
  health.lua: |
    hs = {}
    if obj.status ~= nil then
      if obj.status.phase ~= nil then
        if obj.status.phase == "Pending" then
          hs.status = "Healthy"
          hs.message = obj.status.phase
          return hs
        end
        if obj.status.phase == "Bound" then
          hs.status = "Healthy"
          hs.message = obj.status.phase
          return hs
        end
      end
    end
    hs.status = "Progressing"
    hs.message = "Waiting for certificate"
    return hs`,
					ResourceHealthChecks: []argov1alpha1api.ResourceHealthCheck{{
						Group: "certmanager.k8s.io",
						Kind:  "Certificate",
						Check: `hs = {}
if obj.status ~= nil then
  if obj.status.conditions ~= nil then
    for i, condition in ipairs(obj.status.conditions) do
      if condition.type == "Ready" and condition.status == "False" then
        hs.status = "Degraded"
        hs.message = condition.message
        return hs
      end
      if condition.type == "Ready" and condition.status == "True" then
        hs.status = "Healthy"
        hs.message = condition.message
        return hs
      end
    end
  end
end
hs.status = "Progressing"
hs.message = "Waiting for certificate"
return hs`,
					}},
				},
			}

			Expect(k8sClient.Create(ctx, alphaArgoCD)).To(Succeed())

			By("verifying Argo CD is available via v1beta1 API")
			argoCDBeta := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCDBeta), argoCDBeta)).To(Succeed())

			Eventually(argoCDBeta, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying v1beta1 has resource health check")
			betaRHA := argoCDBeta.Spec.ResourceHealthChecks[0]
			Expect(betaRHA.Group).To(Equal("certmanager.k8s.io"))
			Expect(betaRHA.Kind).To(Equal("Certificate"))
			Expect(betaRHA.Check).To(Equal(`hs = {}
if obj.status ~= nil then
  if obj.status.conditions ~= nil then
    for i, condition in ipairs(obj.status.conditions) do
      if condition.type == "Ready" and condition.status == "False" then
        hs.status = "Degraded"
        hs.message = condition.message
        return hs
      end
      if condition.type == "Ready" and condition.status == "True" then
        hs.status = "Healthy"
        hs.message = condition.message
        return hs
      end
    end
  end
end
hs.status = "Progressing"
hs.message = "Waiting for certificate"
return hs`))

			// In the kuttl test, the fine step tested whether ArgoCD v1beta1 API contained .spec.resourcesCustomizations.
			// Since this field doesn't exist in the ArgoCD v1beta1 API, it cannot ever fail. So it has been removed.

		})

	})
})
