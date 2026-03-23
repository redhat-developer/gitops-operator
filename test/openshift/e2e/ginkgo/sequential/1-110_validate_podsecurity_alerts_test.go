package sequential

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-110_validate_podsecurity_alerts", func() {

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
		})

		It("verifies openshift-gitops: operator sets podSecurityLabelSync and OpenShift populates pod-security label keys", func() {
			gitopsNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "openshift-gitops",
				},
			}
			Eventually(gitopsNS).Should(k8sFixture.ExistByName())

			By("GitOps operator ensures security.openshift.io/scc.podSecurityLabelSync=true")
			Eventually(gitopsNS, "5m", "5s").Should(
				k8sFixture.HaveLabelWithValue("security.openshift.io/scc.podSecurityLabelSync", "true"))

			By("OpenShift pod security label syncer sets pod-security.kubernetes.io/* (values depend on OCP version; only non-empty keys are asserted)")
			for _, key := range []string{
				"pod-security.kubernetes.io/audit",
				"pod-security.kubernetes.io/audit-version",
				"pod-security.kubernetes.io/enforce",
				"pod-security.kubernetes.io/enforce-version",
				"pod-security.kubernetes.io/warn",
				"pod-security.kubernetes.io/warn-version",
			} {
				labelKey := key
				Eventually(func() bool {
					k8sClient, _ := fixtureUtils.GetE2ETestKubeClient()
					ns := &corev1.Namespace{}
					if err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "openshift-gitops"}, ns); err != nil {
						return false
					}
					if ns.Labels == nil {
						return false
					}
					return ns.Labels[labelKey] != ""
				}).WithTimeout(5*time.Minute).WithPolling(5*time.Second).Should(BeTrue(), "expected label %s to be set by OpenShift", labelKey)
			}
		})

	})

})
