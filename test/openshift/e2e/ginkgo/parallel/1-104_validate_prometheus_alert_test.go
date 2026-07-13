package parallel

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-104_validate_prometheus_alert", func() {

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
		})

		It("verify that openshift gitops operator servicemonitor exists in openshift-gitops-operator namespace, and has the expected values", func() {

			if fixture.EnvLocalRun() || fixture.EnvNonOLM() {
				Skip("this test requires the operator to installed via OLM to openshift-operators namespace")
				return
			}

			sm := &monitoringv1.ServiceMonitor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-operator-metrics-monitor",
					Namespace: "openshift-gitops-operator",
				},
			}
			Eventually(sm).Should(k8sFixture.ExistByName())
			serverName := "openshift-gitops-operator-metrics-service.openshift-gitops-operator.svc"
			Expect(sm.Spec.Endpoints).To(Equal([]monitoringv1.Endpoint{{
				Authorization: &monitoringv1.SafeAuthorization{
					Type: "Bearer",
					Credentials: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "openshift-gitops-operator-metrics-monitor-bearer-token",
						},
						Key: "token",
					},
				},
				Interval: monitoringv1.Duration("30s"),
				Path:     "/metrics",
				Port:     "metrics",
				Scheme:   "https",
				TLSConfig: &monitoringv1.TLSConfig{
					SafeTLSConfig: monitoringv1.SafeTLSConfig{
						CA: monitoringv1.SecretOrConfigMap{
							ConfigMap: &corev1.ConfigMapKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "openshift-gitops-operator-metrics-monitor-ca-bundle",
								},
								Key: "service-ca.crt",
							},
						},
						ServerName: &serverName,
					},
				},
			}}))

			Expect(sm.Spec.NamespaceSelector).To(Equal(monitoringv1.NamespaceSelector{}))
			Expect(sm.Spec.Selector).To(Equal(metav1.LabelSelector{
				MatchLabels: map[string]string{
					"control-plane": "gitops-operator",
				},
			}))

			bearerTokenSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-operator-metrics-monitor-bearer-token",
					Namespace: "openshift-gitops-operator",
				},
			}
			Eventually(bearerTokenSecret).Should(k8sFixture.ExistByName())
			Expect(bearerTokenSecret.Type).To(Equal(corev1.SecretTypeOpaque))
			Expect(bearerTokenSecret.Data).To(HaveKey("token"))
			Expect(bearerTokenSecret.Data).To(HaveKey("expiry"))

			expiry, err := time.Parse(time.RFC3339, string(bearerTokenSecret.Data["expiry"]))
			Expect(err).NotTo(HaveOccurred())
			Expect(expiry.After(time.Now())).To(BeTrue())
		})
	})
})
