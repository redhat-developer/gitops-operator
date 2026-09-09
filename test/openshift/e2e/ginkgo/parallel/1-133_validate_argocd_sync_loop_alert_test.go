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

	Context("1-133_validate_argocd_sync_loop_alert", func() {

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
		})

		It("verifying PrometheusRule gitops-operator-argocd-sync-loop-alerts exists and has expected values", Label("openshift"), func() {

			By("checking OpenShift GitOps ArgoCD instance is available")

			argocd, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())
			Eventually(argocd, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying PrometheusRule gitops-operator-argocd-sync-loop-alerts exists and has expected values")
			alertRule := &monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gitops-operator-argocd-sync-loop-alerts",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(alertRule).Should(k8sFixture.ExistByName())

			Expect(alertRule.Spec.Groups).To(Equal([]monitoringv1.RuleGroup{{
				Name: "GitOpsOperatorArgoCDSyncLoop",
				Rules: []monitoringv1.Rule{
					{
						Record: "gitops:argocd_app_sync:rate10m",
						Expr:   intstr.FromString(`sum by (name, namespace) (rate(argocd_app_sync_total{namespace="openshift-gitops"}[10m]))`),
					},
					{
						Record: "gitops:argocd_app_sync_failed:rate10m",
						Expr:   intstr.FromString(`sum by (name, namespace) (rate(argocd_app_sync_total{namespace="openshift-gitops",phase=~"Error|Failed"}[10m]))`),
					},
					{
						Alert: "ArgoCDAppSyncLoopWarning",
						Annotations: map[string]string{
							"summary": "Argo CD application is syncing continuously",
							"description": "Argo CD application {{ $labels.name }} in namespace {{ $labels.namespace }} has a sustained sync rate above 0.01/s " +
								"(about one sync every ~100s) for 20m. This often indicates a selfHeal conflict " +
								"(for example HPA fighting declared replicas). Check application sync history, diff, and conflicting controllers.",
						},
						Expr: intstr.FromString(`gitops:argocd_app_sync:rate10m{namespace="openshift-gitops"} > 0.01`),
						For:  ptr.To(monitoringv1.Duration("20m")),
						Labels: map[string]string{
							"severity": "warning",
						},
					},
					{
						Alert: "ArgoCDAppSyncLoopCritical",
						Annotations: map[string]string{
							"summary": "Argo CD application sync loop is aggressive",
							"description": "Argo CD application {{ $labels.name }} in namespace {{ $labels.namespace }} has a sustained sync rate above 0.1/s " +
								"(about one sync every ~10s) for 10m. Investigate conflicting controllers or tight reconcile settings immediately.",
						},
						Expr: intstr.FromString(`gitops:argocd_app_sync:rate10m{namespace="openshift-gitops"} > 0.1`),
						For:  ptr.To(monitoringv1.Duration("10m")),
						Labels: map[string]string{
							"severity": "critical",
						},
					},
					{
						Alert: "ArgoCDAppSyncFailureLoop",
						Annotations: map[string]string{
							"summary":     "Argo CD application syncs are failing repeatedly",
							"description": "Argo CD application {{ $labels.name }} in namespace {{ $labels.namespace }} has a sustained failed sync rate above 0.005/s for 15m. Investigate the application operation status and sync errors.",
						},
						Expr: intstr.FromString(`gitops:argocd_app_sync_failed:rate10m{namespace="openshift-gitops"} > 0.005`),
						For:  ptr.To(monitoringv1.Duration("15m")),
						Labels: map[string]string{
							"severity": "warning",
						},
					},
				},
			}}))

			By("verifying existing OutOfSync alert rule is unchanged")
			outOfSyncRule := &monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gitops-operator-argocd-alerts",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(outOfSyncRule).Should(k8sFixture.ExistByName())
			Expect(outOfSyncRule.Spec.Groups).ToNot(BeEmpty())
			Expect(outOfSyncRule.Spec.Groups[0].Rules[0].Alert).To(Equal("ArgoCDSyncAlert"))
		})
	})
})
