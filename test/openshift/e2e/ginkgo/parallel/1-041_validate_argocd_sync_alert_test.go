package parallel

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-041_validate_argocd_sync_alert", func() {

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
		})

		It("verifying PrometheusRule gitops-operator-argocd-alerts exists and has expected values", func() {

			By("checking OpenShift GitOps ArgoCD instance is available")

			argocd, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())
			Eventually(argocd, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying PrometheusRule gitops-operator-argocd-alerts exists and has expected values")
			alertRule := &monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gitops-operator-argocd-alerts",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(alertRule).Should(k8sFixture.ExistByName())

			Expect(alertRule.Spec.Groups).To(Equal([]monitoringv1.RuleGroup{{
				Name: "GitOpsOperatorArgoCD",
				Rules: []monitoringv1.Rule{
					{
						Alert: "ArgoCDSyncAlert",
						Annotations: map[string]string{
							"summary":     "Argo CD application is out of sync",
							"description": "Argo CD application {{ $labels.name }} is out of sync. Check ArgoCDSyncAlert status, this alert is designed to notify that an application managed by Argo CD is out of sync.",
						},
						Expr: intstr.FromString(`argocd_app_info{namespace="openshift-gitops",sync_status="OutOfSync"} > 0`),
						For:  ptr.To(monitoringv1.Duration("5m")),
						Labels: map[string]string{
							"severity": "warning",
						},
					},
				},
			}}))

		})
	})
})
