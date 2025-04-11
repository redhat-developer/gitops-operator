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
	deplFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	nodeFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/node"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-069_validate_redis_secure_comm_autotls_ha", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifying when HA is enabled that Argo CD starts successfully in HA mode, and that AutoTLS can be enabled", func() {

			By("verifying we are running on a cluster with at least 3 nodes. This is required for Redis HA")
			nodeFixture.ExpectHasAtLeastXNodes(3)

			// Note: Redis HA requires a cluster which contains multiple nodes

			By("creating simple namespace-scoped Argo CD instance with HA enabled")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					HA: argov1beta1api.ArgoCDHASpec{
						Enabled: true,
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			expectComponentsAreRunning := func() {

				By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
				Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

				deploymentsShouldExist := []string{"argocd-redis-ha-haproxy", "argocd-server", "argocd-repo-server"}
				for _, depl := range deploymentsShouldExist {
					depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: depl, Namespace: ns.Name}}
					Eventually(depl).Should(k8sFixture.ExistByName())
					Eventually(depl).Should(deplFixture.HaveReplicas(1))
					Eventually(depl).Should(deplFixture.HaveReadyReplicas(1))
				}

				statefulsetsShouldExist := []string{"argocd-redis-ha-server", "argocd-application-controller"}
				for _, ss := range statefulsetsShouldExist {

					replicas := 1
					if ss == "argocd-redis-ha-server" {
						replicas = 3
					}

					statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: ss, Namespace: ns.Name}}
					Eventually(statefulSet).Should(k8sFixture.ExistByName())
					Eventually(statefulSet).Should(statefulsetFixture.HaveReplicas(replicas))
					Eventually(statefulSet).Should(statefulsetFixture.HaveReadyReplicas(replicas))
				}

			}

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			expectComponentsAreRunning()

			By("enabling redis HA autoTLS for openshift")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Redis.AutoTLS = "openshift"
			})

			expectComponentsAreRunning()

			By("verifying Redis TLS Secret exists and has data")
			redisTLSSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "argocd-operator-redis-tls", Namespace: ns.Name}}
			Eventually(redisTLSSecret).Should(k8sFixture.ExistByName())

			Expect(string(redisTLSSecret.Type)).To(Equal("kubernetes.io/tls"))
			Expect(redisTLSSecret.Data).To(HaveLen(2))

		})

	})
})
