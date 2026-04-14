package sequential

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	psaAudit          = "pod-security.kubernetes.io/audit"
	psaAuditVersion   = "pod-security.kubernetes.io/audit-version"
	psaEnforce        = "pod-security.kubernetes.io/enforce"
	psaEnforceVersion = "pod-security.kubernetes.io/enforce-version"
	psaWarn           = "pod-security.kubernetes.io/warn"
	psaWarnVersion    = "pod-security.kubernetes.io/warn-version"
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

			By("OpenShift PSA label sync: audit and warn must be restricted; *-version present for each set mode. Enforce must be restricted when set (may be omitted by OpenShift)")
			Eventually(func() bool {
				if gitopsNS.Labels == nil {
					GinkgoWriter.Println("[1-110] openshift-gitops metadata.labels: <nil>")
					return false
				}
				l := gitopsNS.Labels

				ok := l[psaAudit] == "restricted" && l[psaWarn] == "restricted" && l[psaAuditVersion] != "" && l[psaWarnVersion] != ""
				if enforceValue := l[psaEnforce]; enforceValue != "" { // enforce may be omitted by OpenShift. If the label is set, it must be restricted and pod-security.kubernetes.io/enforce-version must be non-empty.
					ok = ok && enforceValue == "restricted" && l[psaEnforceVersion] != ""
				}
				keys := make([]string, 0, len(gitopsNS.Labels))
				for k := range gitopsNS.Labels {
					keys = append(keys, k)
				}
				GinkgoWriter.Printf("[1-110] openshift-gitops metadata.labels (%d):\n", len(gitopsNS.Labels))
				for _, k := range keys {
					GinkgoWriter.Printf("    %s=%q\n", k, gitopsNS.Labels[k])
				}
				return ok
			}).WithTimeout(5*time.Minute).WithPolling(5*time.Second).Should(BeTrue(),
				"expected pod-security audit+warn=restricted with non-empty audit-version and warn-version; enforce=restricted+enforce-version when enforce label exists")
		})

	})

})
