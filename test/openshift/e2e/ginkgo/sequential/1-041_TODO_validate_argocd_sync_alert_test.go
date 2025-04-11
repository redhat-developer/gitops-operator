package sequential

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-041_validate_argocd_sync_alert", func() {

		BeforeEach(func() {

			fixture.EnsureSequentialCleanSlate()

		})

		It("PORTING TEST IS BLOCKED DUE TO COREOS DEPENDENCY", func() {

			By("checking OpenShift GitOps ArgoCD instance is available")

			argocd, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())
			Eventually(argocd, "3m", "5s").Should(argocdFixture.BeAvailable())

			// ---
			// apiVersion: monitoring.coreos.com/v1
			// kind: PrometheusRule
			// metadata:
			//   name: gitops-operator-argocd-alerts
			//   namespace: openshift-gitops
			// spec:
			//   groups:
			//   - name: GitOpsOperatorArgoCD
			// 	rules:
			// 	- alert: ArgoCDSyncAlert
			// 	  annotations:
			// 		summary: Argo CD application is out of sync
			// 		description: Argo CD application {{ $labels.name }} is out of sync. Check ArgoCDSyncAlert status, this alert is designed to notify that an application managed by Argo CD is out of sync.
			// 	  expr: argocd_app_info{namespace="openshift-gitops",sync_status="OutOfSync"} > 0
			// 	  for: 5m
			// 	  labels:
			// 		severity: warning

			// promRule := monitoringv1.PrometheusRule{
			// 	ObjectMeta: metav1.ObjectMeta{
			// 		Name:      "gitops-operator-argocd-alerts",
			// 		Namespace: "openshift-gitops",
			// 	},
			// 	Spec: monitoringv1.PrometheusRuleSpec{
			// 		Groups: []monitoringv1.RuleGroup{},
			// 	},
			// }

		})
	})
})
