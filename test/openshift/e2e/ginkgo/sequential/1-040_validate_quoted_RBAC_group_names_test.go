package sequential

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	routeFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/route"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-040-validate_quoted_RBAC_group_names", func() {

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
		})

		AfterEach(func() {

			// Delete the new role we created during the test
			defer func() {
				By("deleting the role we created during the test")
				_, err := argocdFixture.RunArgoCDCLI("proj", "role", "delete", "default", "somerole")
				Expect(err).ToNot(HaveOccurred())
			}()

			fixture.OutputDebugOnFail()

		})

		It("creates a project role 'somerole' and group claim, and verifies group claim contains the expected data", Label("openshift"), func() {

			defaultArgoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())
			Eventually(defaultArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying the argocd-server route in openshift-gitops namespace has been admitted, so avoid short race condition where Argo CD is deployed, but Route isn't available yet, so it can't be used to log in")
			serverRoute := &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-server",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(serverRoute).Should(k8sFixture.ExistByName())
			Eventually(serverRoute).Should(routeFixture.HaveAdmittedIngress())

			By("logging in to Argo CD instance")
			Expect(argocdFixture.LogInToDefaultArgoCDInstance()).To(Succeed())

			By("Creating a new 'somerole' role in default project")
			output, err := argocdFixture.RunArgoCDCLI("proj", "role", "create", "default", "somerole")
			Expect(err).ToNot(HaveOccurred())

			Expect(output).To(ContainSubstring("Role 'somerole' created"))

			By("waiting for Argo CD to verify the role exists before we add to it (there seems to be some kind of intermittent race condition here in Argo CD itself, where create succeeds in the previous step, but we received 503 in the next step)")
			Eventually(func() bool {
				output, err := argocdFixture.RunArgoCDCLI("proj", "role", "get", "default", "somerole")
				if err != nil {
					GinkgoWriter.Println("error:", err)
					return false
				}

				return strings.Contains(output, "Role Name:")

			}, "30s", "5s").Should(BeTrue())

			By("adding a group claim to the somerole role")
			output, err = argocdFixture.RunArgoCDCLI("proj", "role", "add-group", "default", "somerole", "\"CN=foo,OU=bar,O=baz\"")
			Expect(err).ToNot(HaveOccurred())

			Expect(output).To(ContainSubstring("added to role 'somerole'"))

		})

	})

})
