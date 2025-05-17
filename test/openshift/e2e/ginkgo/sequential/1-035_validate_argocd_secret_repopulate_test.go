package sequential

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	secretFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/secret"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-035-validate_argocd_secret_repopulate", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {

			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies 'argocd-secret' secret is regenerated and we are able to login using that Secret", func() {

			By("checking OpenShift GitOps ArgoCD instance is available")
			argocd, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())
			Eventually(argocd, "4m", "5s").Should(argocdFixture.BeAvailable())

			By("removing data from argocd-secret, to check that it is regenerated")
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "argocd-secret", Namespace: "openshift-gitops"}}
			secretFixture.Update(secret, func(s *corev1.Secret) {
				s.Data = nil
			})

			By("verifying that Secret repopulates")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)
				if err != nil {
					GinkgoWriter.Println(err)
					return false
				}

				if len(secret.Data) == 0 {
					return false
				}

				return true
			}).Should(BeTrue())

			Eventually(argocd, "4m", "5s").Should(argocdFixture.BeAvailable())

			if !fixture.EnvLocalRun() {

				// Skip verifying operator deployment when we are running the operator locally
				By("verifying operator Deployment is ready")

				depl := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "openshift-gitops-operator-controller-manager",
						Namespace: "openshift-gitops-operator",
					},
				}
				Eventually(depl, "1m", "5s").Should(deploymentFixture.HaveReadyReplicas(1))
			}

			Expect(argocdFixture.LogInToDefaultArgoCDInstance()).To(Succeed())
		})
	})
})
