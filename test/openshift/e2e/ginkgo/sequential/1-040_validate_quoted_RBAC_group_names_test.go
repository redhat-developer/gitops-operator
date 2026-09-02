package sequential

import (
	"context"
	"strings"

	"github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-040-validate_quoted_RBAC_group_names", func() {
		// TODO: check if this test can use a new ArgoCD instance instead of the default openshift-gitops instance

		var (
			cancelPortForward func()
			cleanupNamespace  func()
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
			cancelPortForward = nil
			cleanupNamespace = nil
		})

		AfterEach(func() {
			// Role deletion must happen while port-forward is still alive (before namespace cleanup kills the pods).
			if cancelPortForward != nil {
				By("deleting the role we created during the test")
				_, err := argocdFixture.RunArgoCDCLI("proj", "role", "delete", "default", "somerole")
				Expect(err).ToNot(HaveOccurred())
				cancelPortForward()
				cancelPortForward = nil
			}

			if cleanupNamespace != nil {
				cleanupNamespace()
				cleanupNamespace = nil
			}

			fixture.OutputDebugOnFail()
		})

		It("creates a project role 'somerole' and group claim, and verifies group claim contains the expected data", func() {

			By("creating and checking ArgoCD instance is available")
			namespace, cleanup := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			cleanupNamespace = cleanup
			argocd := &v1beta1.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: namespace.Name},
			}
			Expect(k8sClient.Create(ctx, argocd)).To(Succeed())

			Eventually(argocd, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("logging in to Argo CD instance")
			var err error
			cancelPortForward, err = argocdFixture.LogInToArgoCDInstanceWithoutRoute(argocd.Name, namespace.Name)
			Expect(err).ToNot(HaveOccurred())

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
