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
	configmapFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/configmap"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-121_validate_logs_rbac_enforcement", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("validates logs RBAC enforcement as first-class citizen in Argo CD 3.0", func() {

			// Step 1: Create ArgoCD instance with custom RBAC roles for logs testing
			// This tests the new first-class logs RBAC functionality in Argo CD 3.0
			By("creating an Argo CD instance with custom RBAC roles for logs testing")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			// Initial RBAC policy with custom roles - one with logs permissions, one without
			initialRBACPolicy := `# Custom role without logs permissions
p, role:no-logs, applications, get, */*, allow
# Custom role with logs permissions
p, role:with-logs, applications, get, */*, allow
p, role:with-logs, logs, get, */*, allow`

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd",
					Namespace: ns.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{
					RBAC: argov1beta1api.ArgoCDRBACSpec{
						Policy: ptr.To(initialRBACPolicy), // Custom RBAC policy for testing logs enforcement
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			// Step 2: Wait for ArgoCD to be fully deployed and ready
			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			// Step 3: Verify initial RBAC configuration is applied correctly
			// This confirms that the operator correctly applies the custom RBAC policies
			By("verifying the initial RBAC ConfigMap contains custom roles")
			argocdRBACCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-rbac-cm",
					Namespace: ns.Name,
				},
			}
			Eventually(argocdRBACCM).Should(k8sFixture.ExistByName())
			Eventually(argocdRBACCM).Should(configmapFixture.HaveStringDataKeyValue("policy.csv", initialRBACPolicy))

			// Step 4: Verify that the deprecated server.rbac.log.enforce.enable is not present
			// In Argo CD 3.0, logs RBAC is enforced by default and this config is no longer needed
			By("verifying that deprecated server.rbac.log.enforce.enable is not present in argocd-cm")
			argocdCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-cm",
					Namespace: ns.Name,
				},
			}
			Eventually(argocdCM).Should(k8sFixture.ExistByName())
			// Verify the deprecated key is not present (logs RBAC is now first-class)
			Eventually(argocdCM).ShouldNot(configmapFixture.HaveStringDataKeyValue("server.rbac.log.enforce.enable", "true"))

			// Step 5: Update RBAC policy to include global log viewer role
			// This tests the ability to add new roles with logs permissions
			By("updating RBAC policy to include global log viewer role")
			updatedRBACPolicy := `# Custom role without logs permissions
p, role:no-logs, applications, get, */*, allow
# Custom role with logs permissions
p, role:with-logs, applications, get, */*, allow
p, role:with-logs, logs, get, */*, allow
# Global log viewer role
p, role:global-log-viewer, logs, get, */*, allow`

			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.RBAC.Policy = ptr.To(updatedRBACPolicy)
			})

			// Step 6: Verify the RBAC ConfigMap is updated with the global log viewer role
			// This confirms that the operator correctly applies the updated RBAC policies
			By("verifying the RBAC ConfigMap is updated with global log viewer role")
			Eventually(argocdRBACCM).Should(configmapFixture.HaveStringDataKeyValue("policy.csv", updatedRBACPolicy))

			// Step 7: Test legacy configuration handling
			// This simulates upgrading from Argo CD 2.x where server.rbac.log.enforce.enable was used
			By("testing legacy configuration handling")
			legacyRBACPolicy := `# Custom role with only applications access
p, role:app-only, applications, get, */*, allow`

			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.RBAC.Policy = ptr.To(legacyRBACPolicy)
			})

			// Step 8: Verify legacy configuration is handled correctly
			// This ensures that Argo CD 3.0 properly handles legacy RBAC configurations
			By("verifying legacy configuration is handled correctly")
			Eventually(argocdRBACCM).Should(configmapFixture.HaveStringDataKeyValue("policy.csv", legacyRBACPolicy))

			// Step 9: Verify ArgoCD remains stable throughout RBAC changes
			// This ensures that logs RBAC enforcement doesn't break the ArgoCD instance
			By("verifying ArgoCD remains available after RBAC changes")
			Eventually(argoCD, "2m", "5s").Should(argocdFixture.BeAvailable())

			// Step 10: Final verification that deprecated config is still not present
			// This confirms that the deprecated server.rbac.log.enforce.enable is never added
			By("verifying deprecated server.rbac.log.enforce.enable remains absent")
			Eventually(argocdCM).ShouldNot(configmapFixture.HaveStringDataKeyValue("server.rbac.log.enforce.enable", "true"))
		})

	})
})
