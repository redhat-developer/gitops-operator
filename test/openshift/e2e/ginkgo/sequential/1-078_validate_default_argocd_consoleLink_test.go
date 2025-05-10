package sequential

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	consolev1 "github.com/openshift/api/console/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-078_validate_default_argocd_consoleLink", func() {

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
		})

		It("verifies that DISABLE_DEFAULT_ARGOCD_CONSOLELINK disables the ConsoleLink, and it tolerates improper values and can be re-enabled", func() {

			if fixture.EnvLocalRun() {
				Skip("skipping as LOCAL_RUN is set, which implies we are running the operator locally. When running locally, there is no Subscription or Deployment upon which we can set the DISABLE_DEFAULT_ARGOCD_CONSOLELINK env var")
				return
			}

			By("verifying default openshift-gitops Argo CD and ConsoleLink exist")

			argocd, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())
			Eventually(argocd, "2m", "5s").Should(argocdFixture.BeAvailable())

			consoleLink := &consolev1.ConsoleLink{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
			Eventually(consoleLink).Should(k8sFixture.ExistByName())

			By("setting DISABLE_DEFAULT_ARGOCD_CONSOLELINK to true in operator")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("DISABLE_DEFAULT_ARGOCD_CONSOLELINK", "true")

			By("verifying ConsoleLink is deleted")

			Eventually(consoleLink, "60s", "5s").Should(k8sFixture.NotExistByName())
			Consistently(consoleLink).Should(k8sFixture.NotExistByName())

			By("verifying DISABLE_DEFAULT_ARGOCD_CONSOLELINK has the value 'true'")
			Eventually(func() bool {
				val, err := fixture.GetEnvInOperatorSubscriptionOrDeployment("DISABLE_DEFAULT_ARGOCD_CONSOLELINK")
				if err != nil {
					GinkgoWriter.Println(err)
					return false
				}
				if val == nil {
					return false
				}
				return *val == "true"
			}).Should(BeTrue(), "DISABLE_DEFAULT_ARGOCD_CONSOLELINK should be true")

			By("setting DISABLE_DEFAULT_ARGOCD_CONSOLELINK to false")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("DISABLE_DEFAULT_ARGOCD_CONSOLELINK", "false")

			By("verifying ConsoleLink exists")

			Eventually(consoleLink, "60s", "5s").Should(k8sFixture.ExistByName())

			By("verifying DISABLE_DEFAULT_ARGOCD_CONSOLELINK is 'false'")

			Eventually(func() bool {
				val, err := fixture.GetEnvInOperatorSubscriptionOrDeployment("DISABLE_DEFAULT_ARGOCD_CONSOLELINK")
				if err != nil {
					GinkgoWriter.Println(err)
					return false
				}
				if val == nil {
					return false
				}
				return *val == "false"
			}).Should(BeTrue(), "DISABLE_DEFAULT_ARGOCD_CONSOLELINK should be false")

			By("setting DISABLE_DEFAULT_ARGOCD_CONSOLELINK to an empty value")

			fixture.SetEnvInOperatorSubscriptionOrDeployment("DISABLE_DEFAULT_ARGOCD_CONSOLELINK", "")

			By("verifying ConsoleLink continues to exist")

			Eventually(consoleLink, "60s", "5s").Should(k8sFixture.ExistByName())
			Consistently(consoleLink).Should(k8sFixture.ExistByName())

			By("verifying DISABLE_DEFAULT_ARGOCD_CONSOLELINK has an empty value")

			Eventually(func() bool {
				val, err := fixture.GetEnvInOperatorSubscriptionOrDeployment("DISABLE_DEFAULT_ARGOCD_CONSOLELINK")
				if err != nil {
					GinkgoWriter.Println(err)
					return false
				}
				if val == nil {
					return false
				}
				return *val == ""
			}).Should(BeTrue(), "DISABLE_DEFAULT_ARGOCD_CONSOLELINK should be empty, but exist")

			By("cleaning up operator and ensuring ConsoleLink exists")

			Expect(fixture.RestoreSubcriptionToDefault()).To(Succeed())

			Eventually(consoleLink).Should(k8sFixture.ExistByName())
			Consistently(consoleLink).Should(k8sFixture.ExistByName())

		})

	})

})
