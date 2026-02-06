package sequential

import (
	"context"

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
				invalidImage        = "test-image"
				prometheusRuleName  = "gitops-operator-argocd-alerts"
				clusterInstanceName = "openshift-gitops"
			)

			ruleCluster := &monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{Name: prometheusRuleName, Namespace: nsCluster.Name},
			}
			ruleNamespaced := &monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{Name: prometheusRuleName, Namespace: nsNamespaced.Name},
			}

			uwmConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-monitoring-config", Namespace: "openshift-monitoring"},
				Data:       map[string]string{"config.yaml": "enableUserWorkload: true"},
			}
			cmKey := client.ObjectKey{Name: uwmConfigMap.Name, Namespace: uwmConfigMap.Namespace}

			By("enabling user workload monitoring in the cluster monitoring config map")
			existingCM := &corev1.ConfigMap{}
			err := k8sClient.Get(ctx, cmKey, existingCM)

			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, uwmConfigMap)
			})

			if err == nil {
				existingCM.Data = uwmConfigMap.Data
				Expect(k8sClient.Update(ctx, existingCM)).To(Succeed(), "Failed to update existing UWM ConfigMap")
			} else {
				Expect(k8sClient.Create(ctx, uwmConfigMap)).To(Succeed(), "Failed to create UWM ConfigMap")
			}

			By("enabling monitoring on the cluster Argo CD instance and setting an invalid image to trigger alerts")
			argoCDCluster := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: clusterInstanceName, Namespace: nsCluster.Name},
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCDCluster), argoCDCluster)).To(Succeed())

			//restore the cluster instance even if the test fails halfway through it
			DeferCleanup(func() {
				By("restoring the default image and disabling monitoring on cluster Argo CD instance (Cleanup)")
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCDCluster), argoCDCluster)
				argocdFixture.Update(argoCDCluster, func(ac *argov1beta1api.ArgoCD) {
					ac.Spec.ApplicationSet.Image = ""
					ac.Spec.Monitoring.DisableMetrics = ptr.To(true)
				})
			})

			argocdFixture.Update(argoCDCluster, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ApplicationSet = &argov1beta1api.ArgoCDApplicationSet{Image: invalidImage}
				ac.Spec.Monitoring = argov1beta1api.ArgoCDMonitoringSpec{DisableMetrics: ptr.To(false)}
			})

			By("creating a namespaced Argo CD instance with monitoring enabled")
			argoCDNamespaced := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: nsNamespaced.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					ApplicationSet: &argov1beta1api.ArgoCDApplicationSet{Image: invalidImage},
					Monitoring:     argov1beta1api.ArgoCDMonitoringSpec{DisableMetrics: ptr.To(false)},
				},
			}
			Expect(k8sClient.Create(ctx, argoCDNamespaced)).To(Succeed())

			//the verification
			By("waiting for the Argo CD instances to become available")
			Eventually(argoCDCluster, "5m").Should(argocdFixture.BeAvailable())
			Eventually(argoCDNamespaced, "5m").Should(argocdFixture.BeAvailable())

			By("verifying the operator created the expected PrometheusRules")
			Eventually(ruleCluster, "5m").Should(k8sFixture.ExistByName(), "PrometheusRule should be created in cluster namespace")
			Eventually(ruleNamespaced, "5m").Should(k8sFixture.ExistByName(), "PrometheusRule should be created in test namespace")

			By("verifying the ApplicationSet deployments are present (likely in a crash loop due to the invalid image)")
			appSetDeplCluster := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: clusterInstanceName + "-applicationset-controller", Namespace: nsCluster.Name}}
			Eventually(appSetDeplCluster).Should(k8sFixture.ExistByName())

			By("disabling monitoring and restoring the default image on the cluster Argo CD instance")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCDCluster), argoCDCluster)).To(Succeed())
			argocdFixture.Update(argoCDCluster, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ApplicationSet.Image = ""
				ac.Spec.Monitoring.DisableMetrics = ptr.To(true)
			})

			By("disabling monitoring on the namespaced Argo CD instance")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCDNamespaced), argoCDNamespaced)).To(Succeed())
			argocdFixture.Update(argoCDNamespaced, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Monitoring.DisableMetrics = ptr.To(true)
			})

			By("verifying the PrometheusRules are removed")
			Eventually(ruleCluster, "5m").Should(k8sFixture.NotExistByName(), "Cluster PrometheusRule should be deleted")
			Eventually(ruleNamespaced, "5m").Should(k8sFixture.NotExistByName(), "Namespaced PrometheusRule should be deleted")

		})
	})
})
