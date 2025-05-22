package parallel

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	consolev1 "github.com/openshift/api/console/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-003_validate_console_link", func() {

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
		})

		It("verifies ConsoleLink exists and has expected content", func() {

			consoleLink := &consolev1.ConsoleLink{ObjectMeta: metav1.ObjectMeta{
				Name: "argocd",
			}}
			Eventually(consoleLink).Should(k8sFixture.ExistByName())
			Expect(string(consoleLink.Spec.Location)).To(Equal("ApplicationMenu"))
			Expect(consoleLink.Spec.Text).To(Equal("Cluster Argo CD"))

		})

	})
})
