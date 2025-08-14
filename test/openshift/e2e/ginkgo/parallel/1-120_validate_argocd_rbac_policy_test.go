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
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-120_validate_argocd_rbac_policy", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("validates Dex SSO RBAC migration from encoded sub claims to federated_claims.user_id", func() {

			// Step 1: Create ArgoCD instance with Dex SSO and legacy RBAC policies
			// This simulates an Argo CD 2.x installation with encoded sub claims in RBAC
			By("creating an Argo CD instance with Dex SSO and legacy RBAC policies")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			// Legacy RBAC policies using encoded sub claims (simulating Argo CD 2.x)
			// These encoded strings represent user identities in the old format:
			// - ChdleGFtcGxlQGFyZ29wcm9qLmlvEgJkZXhfY29ubl9pZA = test@example.com dex_conn_id
			// - QWRtaW5AZXhhbXBsZS5jb20gZGV4X2Nvbm5faWQ = admin@example.com dex_conn_id
			legacyRBACPolicy := `# Legacy policies using encoded sub claims (simulating Argo CD 2.x)
g, ChdleGFtcGxlQGFyZ29wcm9qLmlvEgJkZXhfY29ubl9pZA, role:test-role
p, ChdleGFtcGxlQGFyZ29wcm9qLmlvEgJkZXhfY29ubl9pZA, applications, get, */*, allow
p, ChdleGFtcGxlQGFyZ29wcm9qLmlvEgJkZXhfY29ubl9pZA, logs, get, */*, allow

# Admin user with encoded sub claim
g, QWRtaW5AZXhhbXBsZS5jb20gZGV4X2Nvbm5faWQ, role:admin
p, QWRtaW5AZXhhbXBsZS5jb20gZGV4X2Nvbm5faWQ, *, *, */*, allow

# Group-based policies (these should work in both versions)
g, test-group, role:test-role
g, admin-group, role:admin`

			// Create ArgoCD CR with Dex SSO configuration and legacy RBAC policies
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd",
					Namespace: ns.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{
					SSO: &argov1beta1api.ArgoCDSSOSpec{
						Provider: argov1beta1api.SSOProviderTypeDex,
						Dex: &argov1beta1api.ArgoCDDexSpec{
							Config: `connectors:
- type: mock
  id: mock
  name: Mock
  config:
    users:
    - email: test@example.com
      name: Test User
      groups: ["test-group"]
- type: mock
  id: mock2
  name: Mock2
  config:
    users:
    - email: admin@example.com
      name: Admin User
      groups: ["admin-group"]`,
						},
					},
					RBAC: argov1beta1api.ArgoCDRBACSpec{
						DefaultPolicy:     ptr.To("role:readonly"),  // Default policy for users without specific roles
						PolicyMatcherMode: ptr.To("glob"),           // Use glob pattern matching for policies
						Policy:            ptr.To(legacyRBACPolicy), // Legacy RBAC policies with encoded sub claims
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			// Step 3: Verify Dex server is running properly
			// This ensures the SSO infrastructure is working before testing RBAC migration
			By("verifying argocd-dex-server Deployment is working as expected")
			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-dex-server", Namespace: ns.Name}}
			Eventually(depl).Should(k8sFixture.ExistByName())
			Eventually(depl).Should(deploymentFixture.HaveReplicas(1))
			Eventually(depl).Should(deploymentFixture.HaveReadyReplicas(1))

			// Step 4: Verify initial RBAC configuration contains legacy encoded sub claims
			// This confirms that the operator correctly applies the legacy RBAC policies
			By("verifying the initial RBAC ConfigMap contains legacy encoded sub claims")
			argocdRBACCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-rbac-cm",
					Namespace: ns.Name,
				},
			}
			Eventually(argocdRBACCM).Should(k8sFixture.ExistByName())
			Eventually(argocdRBACCM).Should(configmapFixture.HaveStringDataKeyValue("policy.csv", legacyRBACPolicy))

			// Step 5: Migrate RBAC policies to Argo CD 3.0+ format
			// This simulates upgrading from Argo CD 2.x to 3.0+ where user identities
			// change from encoded sub claims to federated_claims.user_id format
			By("migrating RBAC policies to use federated_claims.user_id format")
			migratedRBACPolicy := `# Migrated policies using federated_claims.user_id (Argo CD 3.0+)
# User identities are now in plain email format instead of encoded sub claims
g, test@example.com, role:test-role
p, test@example.com, applications, get, */*, allow
p, test@example.com, logs, get, */*, allow

# Admin user with federated_claims.user_id
g, admin@example.com, role:admin
p, admin@example.com, *, *, */*, allow

# Group-based policies (these should work in both versions)
g, test-group, role:test-role
g, admin-group, role:admin`

			// Update the ArgoCD CR with the new RBAC policies
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.RBAC.Policy = ptr.To(migratedRBACPolicy)
			})

			// Step 6: Verify the RBAC ConfigMap is updated with the new policies
			// This confirms that the operator correctly applies the migrated RBAC policies
			By("verifying the RBAC ConfigMap is updated with migrated policies")
			Eventually(argocdRBACCM).Should(configmapFixture.HaveStringDataKeyValue("policy.csv", migratedRBACPolicy))

			// Step 7: Verify ArgoCD remains stable after RBAC migration
			// This ensures that the migration doesn't break the ArgoCD instance
			By("verifying ArgoCD remains available after RBAC migration")
			Eventually(argoCD, "2m", "5s").Should(argocdFixture.BeAvailable())

			// Step 8: Verify Dex server continues to function after RBAC migration
			// This ensures that SSO authentication still works with the new RBAC format
			By("verifying argocd-dex-server remains functional after RBAC migration")
			Eventually(depl).Should(deploymentFixture.HaveReplicas(1))
			Eventually(depl).Should(deploymentFixture.HaveReadyReplicas(1))
		})

	})
})
