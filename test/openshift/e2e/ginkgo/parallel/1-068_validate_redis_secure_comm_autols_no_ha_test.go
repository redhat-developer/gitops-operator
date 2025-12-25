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
	deplFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-068_validate_redis_secure_comm_autotls_no_ha", func() {

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
		})

		AfterEach(func() {
			defer cleanupFunc()
			fixture.OutputDebugOnFail(ns)
		})

		It("validates that the operator configures Redis using auto-gen TLS certificates when HA is disabled", func() {

			expectComponentsAreRunning := func() {

				deploymentsShouldExist := []string{"argocd-redis", "argocd-server", "argocd-repo-server"}
				for _, depl := range deploymentsShouldExist {
					depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: depl, Namespace: ns.Name}}
					Eventually(depl).Should(k8sFixture.ExistByName())
					Eventually(depl).Should(deplFixture.HaveReplicas(1))
					Eventually(depl).Should(deplFixture.HaveReadyReplicas(1))
				}

				statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: ns.Name}}
				Eventually(statefulSet).Should(k8sFixture.ExistByName())
				Eventually(statefulSet).Should(statefulsetFixture.HaveReplicas(1))
				Eventually(statefulSet).Should(statefulsetFixture.HaveReadyReplicas(1))
			}

			By("creating simple namespace-scoped Argo CD instance with HA disabled")
			ns, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					HA: argov1beta1api.ArgoCDHASpec{
						Enabled: false,
					},
					Redis: argov1beta1api.ArgoCDRedisSpec{},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for initial non-HA instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())
			expectComponentsAreRunning()

			By("enabling redis autoTLS for openshift on the non-HA instance")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Redis.AutoTLS = "openshift"
			})

			By("waiting for components to reconcile and restart with AutoTLS enabled")
			//wait for components
			expectComponentsAreRunning()

			By("verifying Redis TLS Secret exists and has data")
			redisTLSSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "argocd-operator-redis-tls", Namespace: ns.Name}}
			Eventually(redisTLSSecret).Should(k8sFixture.ExistByName())

			redisDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-redis", Namespace: ns.Name}}

			By("expecting redis-server to eventually have desired container process command/arguments (TLS)")
			expectedString := "--save \"\" --appendonly no --requirepass " + "$(REDIS_PASSWORD)" + " --tls-port 6379 --port 0 --tls-cert-file /app/config/redis/tls/tls.crt --tls-key-file /app/config/redis/tls/tls.key --tls-auth-clients no"

			if !fixture.IsUpstreamOperatorTests() {
				expectedString = "redis-server --protected-mode no " + expectedString
			}
			//wait for the command to be updated
			Eventually(redisDepl).Should(deplFixture.HaveContainerCommandSubstring(expectedString, 0),
				"TLS .spec.template.spec.containers.args for argocd-redis deployment are wrong")

			repoServerDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-repo-server", Namespace: ns.Name}}

			By("expecting repo-server to eventually have desired container process command/arguments (TLS)")
			Eventually(repoServerDepl).Should(deplFixture.HaveContainerCommandSubstring("uid_entrypoint.sh argocd-repo-server --redis argocd-redis."+ns.Name+".svc.cluster.local:6379 --redis-use-tls --redis-ca-certificate /app/config/reposerver/tls/redis/tls.crt --loglevel info --logformat text", 0),
				"TLS .spec.template.spec.containers.command for argocd-repo-server deployment is wrong")

			argocdServerDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-server", Namespace: ns.Name}}

			By("expecting argocd-server to eventually have desired container process command/arguments (TLS)")
			Eventually(argocdServerDepl).Should(deplFixture.HaveContainerCommandSubstring("argocd-server --staticassets /shared/app --dex-server https://argocd-dex-server."+ns.Name+".svc.cluster.local:5556 --repo-server argocd-repo-server."+ns.Name+".svc.cluster.local:8081 --redis argocd-redis."+ns.Name+".svc.cluster.local:6379 --redis-use-tls --redis-ca-certificate /app/config/server/tls/redis/tls.crt --loglevel info --logformat text", 0),
				"TLS .spec.template.spec.containers.command for argocd-server deployment is wrong")

			applicationControllerSS := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: ns.Name}}

			By("expecting application-controller to eventually have desired container process command/arguments (TLS)")
			Eventually(applicationControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("argocd-application-controller --operation-processors 10 --redis argocd-redis."+ns.Name+".svc.cluster.local:6379 --redis-use-tls --redis-ca-certificate /app/config/controller/tls/redis/tls.crt --repo-server argocd-repo-server."+ns.Name+".svc.cluster.local:8081 --status-processors 20 --kubectl-parallelism-limit 10 --loglevel info --logformat text", 0),
				"TLS .spec.template.spec.containers.command for argocd-application-controller statefulsets is wrong")
		})
	})
})
