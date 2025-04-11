package sequential

import (
	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-106_validate_argocd_metrics_controller", func() {

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
		})

		// expectMetricsEnabled verifies that ArgoCD monitoring is enabled in 'openshift-gitops' ns
		expectMetricsEnabled := func() {

			openshiftGitopsNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops"}}
			Eventually(openshiftGitopsNS).Should(k8sFixture.HaveLabelWithValue("openshift.io/cluster-monitoring", "true"))

			// ServiceMonitors

			Eventually(&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops", Namespace: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

			Eventually(&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-repo-server", Namespace: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

			Eventually(&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-server", Namespace: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

			// Roles/Bindings

			Eventually(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-read", Namespace: "openshift-gitops"}}, "30s", "1s").Should(k8sFixture.ExistByName())

			Eventually(&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-prometheus-k8s-read-binding", Namespace: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

			// PrometheusRule
			Eventually(&monitoringv1.PrometheusRule{ObjectMeta: metav1.ObjectMeta{Name: "gitops-operator-argocd-alerts", Namespace: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

		}

		It("verifies Argo CD metrics can be disabled and re-enabled", func() {

			By("verifying Argo CD metrics are enabled by default in openshift-gitops")

			defaultArgoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())
			Eventually(defaultArgoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			expectMetricsEnabled()

			By("disabling metrics via ArgoCD CR .spec.monitoring.disableMetrics")

			argocdFixture.Update(defaultArgoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Monitoring.DisableMetrics = ptr.To(true)
			})

			By("verifying all metrics resources are in disabled state")
			openshiftGitopsNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: defaultArgoCD.Namespace}}

			Eventually(openshiftGitopsNS).Should(k8sFixture.NotHaveLabelWithValue("openshift.io/cluster-monitoring", "true"))
			Consistently(openshiftGitopsNS).Should(k8sFixture.NotHaveLabelWithValue("openshift.io/cluster-monitoring", "true"))

			Eventually(&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops", Namespace: "openshift-gitops"}}).Should(k8sFixture.NotExistByName())

			Eventually(&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-repo-server", Namespace: "openshift-gitops"}}).Should(k8sFixture.NotExistByName())

			Eventually(&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-server", Namespace: "openshift-gitops"}}).Should(k8sFixture.NotExistByName())

			Eventually(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-read", Namespace: "openshift-gitops"}}).Should(k8sFixture.NotExistByName())

			Eventually(&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-prometheus-k8s-read-binding", Namespace: "openshift-gitops"}}).Should(k8sFixture.NotExistByName())

			Eventually(&monitoringv1.PrometheusRule{ObjectMeta: metav1.ObjectMeta{Name: "gitops-operator-argocd-alerts", Namespace: "openshift-gitops"}}).Should(k8sFixture.NotExistByName())

			By("re-enabling metrics")
			argocdFixture.Update(defaultArgoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Monitoring.DisableMetrics = ptr.To(false)
			})

			By("verifying metrics are re-enabled")

			expectMetricsEnabled()

		})

	})

})
