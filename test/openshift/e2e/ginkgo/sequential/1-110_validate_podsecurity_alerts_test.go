package sequential

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-110_validate_podsecurity_alerts", func() {

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
		})

		It("verifies openshift-gitops: operator sets podSecurityLabelSync and OpenShift sets audit to restricted", func() {
			gitopsNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "openshift-gitops",
				},
			}
			Eventually(gitopsNS).Should(k8sFixture.ExistByName())

			By("GitOps operator ensures security.openshift.io/scc.podSecurityLabelSync=true")
			Eventually(gitopsNS, "5m", "5s").Should(
				k8sFixture.HaveLabelWithValue("security.openshift.io/scc.podSecurityLabelSync", "true"))

			By("OpenShift sets pod-security.kubernetes.io/audit=restricted (pod-security *-version labels vary by cluster and are not asserted)")
			Eventually(gitopsNS, "5m", "5s").Should(
				k8sFixture.HaveLabelWithValue("pod-security.kubernetes.io/audit", "restricted"))
		})

	})

})
