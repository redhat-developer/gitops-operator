package sequential

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	clusterserviceversionFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/clusterserviceversion"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	osFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/os"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-085_validate_dynamic_plugin_installation", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("enables Dynamic plugin via modifying CSV, then verifies the gitops plugin resources are installed as expected", func() {

			if fixture.EnvNonOLM() {
				Skip("Skipping test as NON_OLM env var is set. This test requires operator to running via CSV.")
				return
			}

			if fixture.EnvLocalRun() {
				Skip("Skipping test as LOCAL_RUN env var is set. There is no CSV to modify in this case.")
				return
			}

			// Find CSV
			var csv *olmv1alpha1.ClusterServiceVersion
			var csvList olmv1alpha1.ClusterServiceVersionList
			Expect(k8sClient.List(ctx, &csvList, client.InNamespace("openshift-gitops-operator"))).To(Succeed())

			for idx := range csvList.Items {
				idxCSV := csvList.Items[idx]
				if strings.Contains(idxCSV.Name, "gitops-operator") {
					csv = &idxCSV
					break
				}
			}
			Expect(csv).ToNot(BeNil())

			// At the end of the test, ensure the env var is removed
			defer func() {
				Expect(fixture.RemoveDynamicPluginFromCSV(ctx, k8sClient)).To(Succeed())
			}()

			var ocVersion string

			output, err := osFixture.ExecCommand("oc", "version")
			Expect(err).ToNot(HaveOccurred())

			for _, line := range strings.Split(output, "\n") {

				if strings.Contains(line, "Server Version:") {
					ocVersion = strings.TrimSpace(line[strings.Index(line, ":")+1:])
					break
				}
			}
			Expect(ocVersion).ToNot(BeEmpty())

			if strings.Contains(ocVersion, "4.15.") {
				Skip("skipping this test as OCP version is 4.15")
				return
			}

			By("adding DYNAMIC_PLUGIN_START_OCP_VERSION to CSV operator Deployment env var list")

			clusterserviceversionFixture.Update(csv, func(csv *olmv1alpha1.ClusterServiceVersion) {

				envList := csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Env
				envList = append(envList, corev1.EnvVar{Name: "DYNAMIC_PLUGIN_START_OCP_VERSION", Value: ocVersion})

				csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Env = envList

			})

			By("verifying the plugin's Deployment, ConfigMap, Secret, Service, and other resources have expected values")

			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: "openshift-gitops"}}
			Eventually(depl).Should(k8sFixture.ExistByName())
			Eventually(depl, "60s", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "httpd-cfg", Namespace: "openshift-gitops"}}
			Eventually(configMap).Should(k8sFixture.ExistByName())

			Expect(configMap).To(
				And(k8sFixture.HaveLabelWithValue("app", "gitops-plugin"),
					k8sFixture.HaveLabelWithValue("app.kubernetes.io/part-of", "gitops-plugin")))

			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "console-serving-cert", Namespace: "openshift-gitops"}}
			Eventually(secret).Should(k8sFixture.ExistByName())

			service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: "openshift-gitops"}}
			Eventually(service).Should(k8sFixture.ExistByName())

			match := false
			for _, port := range service.Spec.Ports {
				if port.Name == "tcp-9001" {
					Expect(port.Port).To(Equal(int32(9001)))
					Expect(string(port.Protocol)).To(Equal("TCP"))
					Expect(port.TargetPort.IntValue()).To(Equal(9001))
					match = true
				}
			}
			Expect(match).To(BeTrue())

		})

	})

})
