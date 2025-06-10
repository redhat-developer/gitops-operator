package sequential

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/route"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-109_validate_reencrypt_termination_policy", func() {

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
		})

		It("ensure the openshift-gitops default argo cd server route has expected TLS  Config values: insecure redirect and reencrypt, and the route ingress is sucessfully admitted", func() {

			By("ensuring that default openshift-gitops has expecter route settings and an admitted ingress")
			openshiftGitOpsArgoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			Eventually(openshiftGitOpsArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			serverRoute := &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-server",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(serverRoute).Should(k8sFixture.ExistByName())
			Expect(serverRoute.Spec.TLS).To(Equal(&routev1.TLSConfig{
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
				Termination:                   routev1.TLSTerminationReencrypt,
			}))

			Eventually(serverRoute, "3m", "5s").Should(route.HaveAdmittedIngress())

			Expect(serverRoute.Spec.Host).ToNot(BeEmpty())

			// The kuttl test this is based on never passed:

			// Kuttl shows this error when the test is run:
			//     logger.go:42: 16:02:14 | 1-109_validate_reencrypt_termination_policy/2-check-certificate | sh: line 5: [: too many arguments

			// But this doesn't fail the kuttl test.

			// output, err := os.ExecCommand("curl", "--insecure", "-v", serverRoute.Spec.Host)
			// Expect(err).ToNot(HaveOccurred())

			// match := false
			// for _, line := range strings.Split(output, "\n") {
			// 	line = strings.TrimSpace(line)

			// 	if !strings.Contains(line, "issuer:") {
			// 		continue
			// 	}

			// 	fmt.Println("----------------------")
			// 	fmt.Println(line)

			// 	Expect(strings.Contains(line, "root-ca") || strings.Contains(line, "ingress-operator")).To(BeTrue(), "TLS connections rely on the default ingress operator certificate or root-ca")

			// 	match = true
			// }

			// Expect(match).To(BeTrue())

		})
	})

})
