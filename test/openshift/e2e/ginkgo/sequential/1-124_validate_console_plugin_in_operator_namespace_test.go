/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package sequential

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	consolev1 "github.com/openshift/api/console/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-124_validate_console_plugin_in_operator_namespace", func() {

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
		})

		It("verifies that console plugin resources are deployed in the operator namespace, not the default ArgoCD namespace", func() {

			k8sClient, _ := utils.GetE2ETestKubeClient()

			operatorNamespace := "openshift-gitops-operator"

			By("verifying plugin Deployment exists in operator namespace and is ready")
			pluginDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: operatorNamespace}}
			Eventually(pluginDepl, "3m", "5s").Should(k8sFixture.ExistByName())
			Eventually(pluginDepl, "3m", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			By("verifying plugin Service exists in operator namespace")
			pluginSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: operatorNamespace}}
			Eventually(pluginSvc, "60s", "5s").Should(k8sFixture.ExistByName())

			By("verifying plugin ConfigMap exists in operator namespace")
			pluginCM := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "httpd-cfg", Namespace: operatorNamespace}}
			Eventually(pluginCM, "60s", "5s").Should(k8sFixture.ExistByName())

			By("verifying ConsolePlugin CR backend points to operator namespace")
			consolePlugin := &consolev1.ConsolePlugin{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin"}}
			Eventually(consolePlugin, "60s", "5s").Should(k8sFixture.ExistByName())
			err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(consolePlugin), consolePlugin)
			Expect(err).ToNot(HaveOccurred())
			Expect(consolePlugin.Spec.Backend.Service).ToNot(BeNil())
			Expect(consolePlugin.Spec.Backend.Service.Namespace).To(Equal(operatorNamespace))

			By("verifying plugin resources do NOT exist in openshift-gitops namespace")
			oldDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: "openshift-gitops"}}
			Eventually(oldDepl).Should(k8sFixture.NotExistByName())

			oldSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: "openshift-gitops"}}
			Eventually(oldSvc).Should(k8sFixture.NotExistByName())

			oldCM := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "httpd-cfg", Namespace: "openshift-gitops"}}
			Eventually(oldCM).Should(k8sFixture.NotExistByName())
		})
	})
})
