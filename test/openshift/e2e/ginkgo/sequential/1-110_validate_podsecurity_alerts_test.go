package sequential

import (
	"context"

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

		It("verifies openshift-gitops: operator sets podSecurityLabelSync and OpenShift populates pod-security labels", func() {
			gitopsNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "openshift-gitops",
				},
			}
			Eventually(gitopsNS).Should(k8sFixture.ExistByName())

			By("GitOps operator ensures security.openshift.io/scc.podSecurityLabelSync=true")
			Eventually(gitopsNS, "5m", "5s").Should(
				k8sFixture.HaveLabelWithValue("security.openshift.io/scc.podSecurityLabelSync", "true"))

			By("OpenShift populates at least one pod-security.kubernetes.io/* label")
			pssLabelKeys := []string{
				"pod-security.kubernetes.io/audit",
				"pod-security.kubernetes.io/audit-version",
				"pod-security.kubernetes.io/enforce",
				"pod-security.kubernetes.io/enforce-version",
				"pod-security.kubernetes.io/warn",
				"pod-security.kubernetes.io/warn-version",
			}
			Eventually(func() bool {
				k8sClient, _ := fixtureUtils.GetE2ETestKubeClient()
				ns := &corev1.Namespace{}
				if err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "openshift-gitops"}, ns); err != nil {
					return false
				}
				if ns.Labels == nil {
					return false
				}
				for _, key := range pssLabelKeys {
					if ns.Labels[key] != "" {
						return true
					}
				}
				return false
			}, "5m", "5s").Should(BeTrue(), "expected at least one pod-security.kubernetes.io/* label to be set by OpenShift")

			k8sClient, _ := fixtureUtils.GetE2ETestKubeClient()
			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: "openshift-gitops"}, ns)).To(Succeed())
			for _, key := range pssLabelKeys {
				GinkgoWriter.Printf("observed namespace label %s=%q\n", key, ns.Labels[key])
			}
		})

	})

})
