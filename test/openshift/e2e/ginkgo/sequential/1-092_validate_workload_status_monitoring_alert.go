package sequential

import (
	"context"
	"os/exec"
	"strings"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-092_validate_workload_status_monitoring_alert", func() {
		var (
			k8sClient    client.Client
			ctx          context.Context
			nsCluster    *corev1.Namespace
			nsNamespaced *corev1.Namespace
			cleanupFunc  func()
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()

			nsCluster = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops"}}
			nsNamespaced, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
		})

		AfterEach(func() {
			defer cleanupFunc()
			fixture.OutputDebugOnFail(nsNamespaced)
		})

		It("validates monitoring setup, alert rule creation, and teardown", func() {
			const (
				// picking a valid image that exists to avoid ImagePullBackOff
				// but should fail to run as an ApplicationSet controller
				invalidImage        = "quay.io/libpod/alpine:latest"
				prometheusRuleName  = "argocd-component-status-alert"
				clusterInstanceName = "openshift-gitops"
			)

			ruleCluster := &monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{Name: prometheusRuleName, Namespace: nsCluster.Name},
			}
			ruleNamespaced := &monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{Name: prometheusRuleName, Namespace: nsNamespaced.Name},
			}
			appSetDeplCluster := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: clusterInstanceName + "-applicationset-controller", Namespace: nsCluster.Name},
			}
			uwmConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-monitoring-config", Namespace: "openshift-monitoring"},
				Data:       map[string]string{"config.yaml": "enableUserWorkload: true\n"},
			}

			By("labeling the namespace for monitoring")
			// Prometheus will only scrape User Workload namespaces that have this label
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(nsNamespaced), nsNamespaced)
			Expect(err).NotTo(HaveOccurred())

			if nsNamespaced.Labels == nil {
				nsNamespaced.Labels = make(map[string]string)
			}
			nsNamespaced.Labels["openshift.io/cluster-monitoring"] = "true"
			err = k8sClient.Update(ctx, nsNamespaced)
			Expect(err).NotTo(HaveOccurred())

			By("enabling user workload monitoring in the cluster monitoring config map")
			existingCM := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(uwmConfigMap), existingCM)

			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, uwmConfigMap)
			})

			if err == nil {
				existingCM.Data = uwmConfigMap.Data
				Expect(k8sClient.Update(ctx, existingCM)).To(Succeed(), "Failed to update existing UWM ConfigMap")
			} else if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, uwmConfigMap)).To(Succeed(), "Failed to create UWM ConfigMap")
			} else {
				Expect(err).NotTo(HaveOccurred(), "Failed to fetch UWM ConfigMap")
			}

			By("modifying both ArgoCD instances to enable monitoring and break the AppSet image")
			argoCDCluster := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: clusterInstanceName, Namespace: nsCluster.Name},
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCDCluster), argoCDCluster)).To(Succeed())

			// Restore even if the test fails halfway through
			DeferCleanup(func() {
				By("restoring the default image and disabling monitoring on cluster Argo CD instance (Cleanup)")
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCDCluster), argoCDCluster)
				argocdFixture.Update(argoCDCluster, func(ac *argov1beta1api.ArgoCD) {
					ac.Spec.ApplicationSet.Image = ""
					ac.Spec.Monitoring.DisableMetrics = ptr.To(true)
					ac.Spec.Monitoring.Enabled = false
				})
			})

			argocdFixture.Update(argoCDCluster, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ApplicationSet = &argov1beta1api.ArgoCDApplicationSet{Image: invalidImage}
				ac.Spec.Monitoring = argov1beta1api.ArgoCDMonitoringSpec{Enabled: true, DisableMetrics: ptr.To(false)}
			})

			argoCDNamespaced := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: nsNamespaced.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					ApplicationSet: &argov1beta1api.ArgoCDApplicationSet{Image: invalidImage},
					Monitoring:     argov1beta1api.ArgoCDMonitoringSpec{Enabled: true, DisableMetrics: ptr.To(false)},
				},
			}
			Expect(k8sClient.Create(ctx, argoCDNamespaced)).To(Succeed())

			By("waiting for the Argo CD instances to become available")
			Eventually(argoCDCluster, "5m").Should(argocdFixture.BeAvailable())
			Eventually(argoCDNamespaced, "5m").Should(argocdFixture.BeAvailable())

			By("verifying PrometheusRules are created with the correct alerts")
			Eventually(ruleCluster, "3m", "5s").Should(k8sFixture.ExistByName(), "PrometheusRule should be created in cluster namespace")
			Eventually(ruleNamespaced, "3m", "5s").Should(k8sFixture.ExistByName(), "PrometheusRule should be created in test namespace")

			By("verifying the ApplicationSet deployments are present")
			Eventually(appSetDeplCluster).Should(k8sFixture.ExistByName())

			By("verifying the workload degradation alerts are actively firing in Prometheus")
			Eventually(func() bool {
				cmd := exec.Command("oc", "exec", "-n", "openshift-monitoring", "prometheus-k8s-0", "-c", "prometheus", "--", "curl", "-s", "http://localhost:9090/api/v1/alerts")
				outBytes, err := cmd.Output()
				if err != nil {
					GinkgoWriter.Printf("Failed to query Prometheus: %v\n", err)
					return false
				}
				out := string(outBytes)

				// check both default and the custom instance alerts are firing
				hasDefaultAlert := strings.Contains(out, "openshift-gitops-applicationset-controller") &&
					strings.Contains(out, "ApplicationSetControllerNotReady") &&
					strings.Contains(out, "firing")

				hasCustomAlert := strings.Contains(out, "argocd-applicationset-controller") &&
					strings.Contains(out, nsNamespaced.Name) &&
					strings.Contains(out, "ApplicationSetControllerNotReady") &&
					strings.Contains(out, "firing")

				return hasDefaultAlert && hasCustomAlert
			}, "15m", "30s").Should(BeTrue(), "Expected ApplicationSetControllerNotReady alerts to reach 'firing' state for both instances")

			By("disabling monitoring and restoring the default images")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCDCluster), argoCDCluster)).To(Succeed())
			argocdFixture.Update(argoCDCluster, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ApplicationSet.Image = ""
				ac.Spec.Monitoring.Enabled = false
				ac.Spec.Monitoring.DisableMetrics = ptr.To(true)
			})

			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCDNamespaced), argoCDNamespaced)).To(Succeed())
			argocdFixture.Update(argoCDNamespaced, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ApplicationSet.Image = ""
				ac.Spec.Monitoring.Enabled = false
				ac.Spec.Monitoring.DisableMetrics = ptr.To(true)
			})

			By("deleting cluster monitoring config")
			Expect(k8sClient.Delete(ctx, uwmConfigMap)).To(Succeed())

			By("verifying PrometheusRules are deleted")
			Eventually(ruleCluster, "5m").Should(k8sFixture.NotExistByName(), "Cluster PrometheusRule should be deleted")
			Eventually(ruleNamespaced, "5m").Should(k8sFixture.NotExistByName(), "Namespaced PrometheusRule should be deleted")
		})
	})
})
