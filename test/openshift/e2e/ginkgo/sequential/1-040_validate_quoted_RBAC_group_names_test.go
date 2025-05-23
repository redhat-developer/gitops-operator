package sequential

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-040-validate_quoted_RBAC_group_names", func() {

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
		})

		It("creates a project role 'somerole' and group claim, and verifies group claim contains the expected data", func() {

			By("logging in to Argo CD instance")
			Expect(argocdFixture.LogInToDefaultArgoCDInstance()).To(Succeed())

			By("Creating a new 'somerole' role in default project")
			output, err := argocdFixture.RunArgoCDCLI("proj", "role", "create", "default", "somerole")
			Expect(err).ToNot(HaveOccurred())

			// Delete the new role we created during the test
			defer func() {
				By("deleting the role we created during the test")
				_, err = argocdFixture.RunArgoCDCLI("proj", "role", "delete", "default", "somerole")
				Expect(err).ToNot(HaveOccurred())
			}()

			Expect(output).To(ContainSubstring("Role 'somerole' created"))

			By("adding a group claim to the somerole role")
			output, err = argocdFixture.RunArgoCDCLI("proj", "role", "add-group", "default", "somerole", "\"CN=foo,OU=bar,O=baz\"")
			Expect(err).ToNot(HaveOccurred())

			Expect(output).To(ContainSubstring("added to role 'somerole'"))

		})

	})

})
