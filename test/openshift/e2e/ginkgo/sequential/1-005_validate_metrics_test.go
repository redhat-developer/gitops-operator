package sequential

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-005_validate_metrics_test", func() {

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
		})

		It("verifies that default ServiceMonitors exist in openshift-gitops and PrometheusRule ArgoCDSyncAlert exists", func() {

			By("verifying openshift-gitops ServiceMonitor exists and has expected values")
			openshiftGitOpsSM := &monitoringv1.ServiceMonitor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(openshiftGitOpsSM).Should(k8sFixture.ExistByName())
			Expect(openshiftGitOpsSM.Spec).Should(Equal(monitoringv1.ServiceMonitorSpec{
				Endpoints: []monitoringv1.Endpoint{
					{
						Port: "metrics",
					},
				},
				NamespaceSelector: monitoringv1.NamespaceSelector{},
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app.kubernetes.io/name": "openshift-gitops-metrics",
					},
				},
			}))

			By("verifying openshift-gitops-repo-server ServiceMonitor exists and has expected values")
			openshiftGitOpsRepoServerSM := &monitoringv1.ServiceMonitor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-repo-server",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(openshiftGitOpsRepoServerSM).Should(k8sFixture.ExistByName())
			Expect(openshiftGitOpsRepoServerSM.Spec).Should(Equal(monitoringv1.ServiceMonitorSpec{
				Endpoints: []monitoringv1.Endpoint{
					{
						Port: "metrics",
					},
				},
				NamespaceSelector: monitoringv1.NamespaceSelector{},
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app.kubernetes.io/name": "openshift-gitops-repo-server",
					},
				},
			}))

			By("verifying openshift-gitops-server ServiceMonitor exists and has expected values")
			openshiftGitOpsServerSM := &monitoringv1.ServiceMonitor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-server",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(openshiftGitOpsServerSM).Should(k8sFixture.ExistByName())
			Expect(openshiftGitOpsServerSM.Spec).Should(Equal(monitoringv1.ServiceMonitorSpec{
				Endpoints: []monitoringv1.Endpoint{
					{
						Port: "metrics",
					},
				},
				NamespaceSelector: monitoringv1.NamespaceSelector{},
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app.kubernetes.io/name": "openshift-gitops-server-metrics",
					},
				},
			}))

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
