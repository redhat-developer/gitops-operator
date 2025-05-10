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
	"os"
	"time"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deplFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	osFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/os"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-066_validate_redis_secure_comm_no_autotls_no_ha", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("validates that Argo CD components correctly inherit 'argocd-operator-redis-tls' Secret once it is created", func() {

			By("creating simple namespace-scoped Argo CD instance")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			expectComponentsAreRunning := func() {

				time.Sleep(15 * time.Second) // I don't see an easier way to detect when deployment/statefulset controller have reconciled the changes we have made. So instead we just use a long delay.

				deploymentsShouldExist := []string{"argocd-redis", "argocd-server", "argocd-repo-server"}
				for _, depl := range deploymentsShouldExist {
					depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: depl, Namespace: ns.Name}}
					Eventually(depl, "60s", "5s").Should(k8sFixture.ExistByName())
					Eventually(depl, "60s", "5s").Should(deplFixture.HaveReplicas(1))
					Eventually(depl, "60s", "5s").Should(deplFixture.HaveReadyReplicas(1))
				}

				statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: ns.Name}}
				Eventually(statefulSet, "60s", "5s").Should(k8sFixture.ExistByName())
				Eventually(statefulSet, "60s", "5s").Should(statefulsetFixture.HaveReplicas(1))
				Eventually(statefulSet, "60s", "5s").Should(statefulsetFixture.HaveReadyReplicas(1))

			}

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			expectComponentsAreRunning()

			By("generating a test certificate to use with redis, using openssl")

			redis_crt_File, err := os.CreateTemp("", "redis.crt")
			Expect(err).To(BeNil())

			redis_key_File, err := os.CreateTemp("", "redis.key")
			Expect(err).To(BeNil())

			openssl_test_File, err := os.CreateTemp("", "openssl_test.cnf")
			Expect(err).To(BeNil())

			opensslTestCNFContents := "\n[SAN]\nsubjectAltName=DNS:argocd-redis." + ns.Name + ".svc.cluster.local\n[req]\ndistinguished_name=req"

			err = os.WriteFile(openssl_test_File.Name(), ([]byte)(opensslTestCNFContents), 0666)
			Expect(err).To(BeNil())

			_, err = osFixture.ExecCommandWithOutputParam(false, "openssl", "req", "-new", "-x509", "-sha256",
				"-subj", "/C=XX/ST=XX/O=Testing/CN=redis",
				"-reqexts", "SAN",
				"-extensions", "SAN",
				"-config", openssl_test_File.Name(),
				"-keyout", redis_key_File.Name(),
				"-out", redis_crt_File.Name(),
				"-newkey", "rsa:4096",
				"-nodes",
				"-days", "10",
			)
			Expect(err).ToNot(HaveOccurred())

			_, err = osFixture.ExecCommand("oc", "create", "secret", "tls", "argocd-operator-redis-tls", "--key="+redis_key_File.Name(), "--cert="+redis_crt_File.Name(), "-n", ns.Name)
			Expect(err).ToNot(HaveOccurred())

			expectComponentsAreRunning()

			_, err = osFixture.ExecCommand("oc", "annotate", "secret", "argocd-operator-redis-tls", "argocds.argoproj.io/name=argocd", "-n", ns.Name)
			Expect(err).ToNot(HaveOccurred())

			By("verifying that all the components restart successfully once we define the argocd-operator-redis-tls Secret")
			expectComponentsAreRunning()

			redisDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-redis", Namespace: ns.Name}}
			Eventually(redisDepl).Should(k8sFixture.ExistByName())

			By("expecting redis-server to have desired container process command/arguments")

			Expect(redisDepl).To(deplFixture.HaveContainerCommandSubstring("redis-server --protected-mode no --save \"\" --appendonly no --requirepass "+"$(REDIS_PASSWORD)"+" --tls-port 6379 --port 0 --tls-cert-file /app/config/redis/tls/tls.crt --tls-key-file /app/config/redis/tls/tls.key --tls-auth-clients no", 0),
				"TLS .spec.template.spec.containers.args for argocd-redis deployment are wrong")

			repoServerDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-repo-server", Namespace: ns.Name}}
			Eventually(repoServerDepl).Should(k8sFixture.ExistByName())

			By("expecting repo-server to have desired container process command/arguments")
			Expect(repoServerDepl).To(deplFixture.HaveContainerCommandSubstring("uid_entrypoint.sh argocd-repo-server --redis argocd-redis."+ns.Name+".svc.cluster.local:6379 --redis-use-tls --redis-ca-certificate /app/config/reposerver/tls/redis/tls.crt --loglevel info --logformat text", 0),
				"TLS .spec.template.spec.containers.command for argocd-repo-server deployment is wrong")

			argocdServerDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-server", Namespace: ns.Name}}
			Eventually(argocdServerDepl).Should(k8sFixture.ExistByName())

			By("expecting argocd-server to have desired container process command/arguments")
			Expect(argocdServerDepl).To(deplFixture.HaveContainerCommandSubstring("argocd-server --staticassets /shared/app --dex-server https://argocd-dex-server."+ns.Name+".svc.cluster.local:5556 --repo-server argocd-repo-server."+ns.Name+".svc.cluster.local:8081 --redis argocd-redis."+ns.Name+".svc.cluster.local:6379 --redis-use-tls --redis-ca-certificate /app/config/server/tls/redis/tls.crt --loglevel info --logformat text", 0),
				"TLS .spec.template.spec.containers.command for argocd-server deployment is wrong")

			By("expecting application-controller to have desired container process command/arguments")
			applicationControllerSS := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: ns.Name}}
			Eventually(applicationControllerSS).Should(k8sFixture.ExistByName())

			Expect(applicationControllerSS).To(statefulsetFixture.HaveContainerCommandSubstring("argocd-application-controller --operation-processors 10 --redis argocd-redis."+ns.Name+".svc.cluster.local:6379 --redis-use-tls --redis-ca-certificate /app/config/controller/tls/redis/tls.crt --repo-server argocd-repo-server."+ns.Name+".svc.cluster.local:8081 --status-processors 20 --kubectl-parallelism-limit 10 --loglevel info --logformat text", 0),
				"TLS .spec.template.spec.containers.command for argocd-application-controller statefulsets is wrong")

		})

	})
})
