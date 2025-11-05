package parallel

import (
	"context"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-050_validate_sso", func() {

		var (
			ctx         context.Context
			k8sClient   client.Client
			ns          *corev1.Namespace
			cleanupFunc func()
		)

		BeforeEach(func() {

			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		AfterEach(func() {

			fixture.OutputDebugOnFail(ns)

			if cleanupFunc != nil {
				cleanupFunc()
			}
		})

		It("ensures Dex/Keycloak SSO can be enabled and disabled on a namespace-scoped Argo CD instance", func() {

			ns, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			By("creating a new Argo CD instance with dex and openshift oauth enabled")

			newArgoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd",
					Namespace: ns.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{
					SSO: &argov1beta1api.ArgoCDSSOSpec{
						Provider: "dex",
						Dex: &argov1beta1api.ArgoCDDexSpec{
							OpenShiftOAuth: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, newArgoCD)).To(Succeed())

			By("verifying Argo CD is available and Dex is running")

			Eventually(newArgoCD, "5m", "5s").Should(
				SatisfyAll(argocdFixture.BeAvailable(), argocdFixture.HaveSSOStatus("Running")))

			dexDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-dex-server", Namespace: ns.Name}}
			Eventually(dexDepl).Should(k8sFixture.ExistByName())
			Eventually(dexDepl).Should(deploymentFixture.HaveReadyReplicas(1))

			sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-dex-server", Namespace: ns.Name}}
			Eventually(sa).Should(k8sFixture.ExistByName())

			rb := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-dex-server", Namespace: ns.Name}}
			Eventually(rb).Should(k8sFixture.ExistByName())

			r := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-dex-server", Namespace: ns.Name}}
			Eventually(r).Should(k8sFixture.ExistByName())

			service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "argocd-dex-server", Namespace: ns.Name}}
			Eventually(service).Should(k8sFixture.ExistByName())

			By("disabling SSO")

			argocdFixture.Update(newArgoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.SSO = nil
			})

			By("verifying that Argo CD becomes available after disabling SSO, and SSO is disabled")
			Eventually(newArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())
			Eventually(newArgoCD, "3m", "5s").Should(argocdFixture.HaveSSOStatus("Unknown"))

			By("verifying dex resources no longer exist")
			Eventually(dexDepl).Should(k8sFixture.NotExistByName())
			Consistently(dexDepl).Should(k8sFixture.NotExistByName())

			Eventually(sa).Should(k8sFixture.NotExistByName())
			Consistently(sa).Should(k8sFixture.NotExistByName())

			Eventually(rb).Should(k8sFixture.NotExistByName())
			Consistently(rb).Should(k8sFixture.NotExistByName())

			Eventually(r).Should(k8sFixture.NotExistByName())
			Consistently(r).Should(k8sFixture.NotExistByName())

			Eventually(service).Should(k8sFixture.NotExistByName())
			Consistently(service).Should(k8sFixture.NotExistByName())

		})

	})

})
