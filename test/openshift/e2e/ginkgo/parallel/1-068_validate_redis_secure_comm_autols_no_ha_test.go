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
				for _, deplName := range deploymentsShouldExist {
					depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deplName, Namespace: ns.Name}}
					Eventually(depl).Should(k8sFixture.ExistByName())
					Eventually(depl).Should(deplFixture.HaveReadyReplicas(1))
				}

				statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: ns.Name}}
				Eventually(statefulSet).Should(k8sFixture.ExistByName())
				Eventually(statefulSet).Should(statefulsetFixture.HaveReadyReplicas(1))
			}

			By("creating a namespace-scoped Argo CD instance with HA disabled")
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

			By("waiting for the non-HA instance to become available")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())
			expectComponentsAreRunning()

			By("enabling Redis AutoTLS for OpenShift on the instance")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Redis.AutoTLS = "openshift"
			})

			By("waiting for the components to reconcile and restart")
			expectComponentsAreRunning()

			By("verifying the Redis TLS secret exists and contains the correct data")
			redisTLSSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "argocd-operator-redis-tls", Namespace: ns.Name}}
			Eventually(redisTLSSecret).Should(k8sFixture.ExistByName())

			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(redisTLSSecret), redisTLSSecret)).To(Succeed())
			Expect(redisTLSSecret.Type).To(Equal(corev1.SecretTypeTLS), "Secret type should be kubernetes.io/tls")
			Expect(redisTLSSecret.Data).To(HaveLen(2), "Secret should contain exactly 2 data items (tls.key and tls.crt)")

			By("verifying the redis-server deployment has the expected TLS flags")
			redisDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-redis", Namespace: ns.Name}}

			redisTlsFlags := []string{
				"--tls-port 6379",
				"--port 0",
				"--tls-cert-file /app/config/redis/tls/tls.crt",
				"--tls-key-file /app/config/redis/tls/tls.key",
				"--tls-auth-clients no",
			}
			for _, flag := range redisTlsFlags {
				Eventually(redisDepl).Should(deplFixture.HaveContainerCommandSubstring(flag, 0), "Redis missing TLS flag: "+flag)
			}

			By("verifying the repo-server deployment is configured to use TLS")
			repoServerDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-repo-server", Namespace: ns.Name}}

			Eventually(repoServerDepl).Should(deplFixture.HaveContainerCommandSubstring("--redis-use-tls", 0))
			Eventually(repoServerDepl).Should(deplFixture.HaveContainerCommandSubstring("--redis-ca-certificate /app/config/reposerver/tls/redis/tls.crt", 0))

			By("verifying the argocd-server deployment is configured to use TLS")
			argocdServerDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-server", Namespace: ns.Name}}

			Eventually(argocdServerDepl).Should(deplFixture.HaveContainerCommandSubstring("--redis-use-tls", 0))
			Eventually(argocdServerDepl).Should(deplFixture.HaveContainerCommandSubstring("--redis-ca-certificate /app/config/server/tls/redis/tls.crt", 0))

			By("verifying the application-controller statefulset is configured to use TLS")
			applicationControllerSS := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: ns.Name}}

			Eventually(applicationControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("--redis-use-tls", 0))
			Eventually(applicationControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("--redis-ca-certificate /app/config/controller/tls/redis/tls.crt", 0))
		})
	})
})
