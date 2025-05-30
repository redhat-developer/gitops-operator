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

		It("verifies openshift-gitops Namespace has expected pod-security labels", func() {

			gitopsNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "openshift-gitops",
				},
			}
			Eventually(gitopsNS).Should(k8sFixture.ExistByName())

			Eventually(gitopsNS).Should(k8sFixture.HaveLabelWithValue("pod-security.kubernetes.io/audit", "restricted"))
			Eventually(gitopsNS).Should(k8sFixture.HaveLabelWithValue("pod-security.kubernetes.io/audit-version", "latest"))
			Eventually(gitopsNS).Should(k8sFixture.HaveLabelWithValue("pod-security.kubernetes.io/enforce", "restricted"))
			Eventually(gitopsNS).Should(k8sFixture.HaveLabelWithValue("pod-security.kubernetes.io/enforce-version", "v1.29"))
			Eventually(gitopsNS).Should(k8sFixture.HaveLabelWithValue("pod-security.kubernetes.io/warn", "restricted"))
			Eventually(gitopsNS).Should(k8sFixture.HaveLabelWithValue("pod-security.kubernetes.io/warn-version", "latest"))
		})

	})

})
