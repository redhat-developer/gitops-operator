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
	gitopsoperatorv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
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

		It("cleans up plugin resources from the old namespace when they exist (simulating upgrade from older version)", func() {

			k8sClient, _ := utils.GetE2ETestKubeClient()
			ctx := context.Background()
			oldNamespace := "openshift-gitops"
			operatorNamespace := "openshift-gitops-operator"

			By("waiting for plugin deployment to be ready in operator namespace before proceeding")
			readyDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: operatorNamespace}}
			Eventually(readyDepl, "5m", "5s").Should(k8sFixture.ExistByName())
			Eventually(readyDepl, "3m", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			By("deleting GitopsService CR to prepare for recreation trigger")
			Expect(k8sClient.Delete(ctx, &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			})).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(
					&gitopsoperatorv1alpha1.GitopsService{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}}),
					&gitopsoperatorv1alpha1.GitopsService{})
				return err != nil
			}, "60s", "5s").Should(BeTrue())

			By("creating fake plugin resources in the old namespace to simulate a pre-upgrade installation")
			oldDepl := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gitops-plugin",
					Namespace: oldNamespace,
					Labels:    map[string]string{"app": "gitops-plugin"},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "gitops-plugin"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "gitops-plugin"}},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "gitops-plugin", Image: "fake-image:latest"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, oldDepl)).To(Succeed())

			oldSvc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gitops-plugin",
					Namespace: oldNamespace,
					Labels:    map[string]string{"app": "gitops-plugin"},
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": "gitops-plugin"},
					Ports:    []corev1.ServicePort{{Port: 9001, Protocol: corev1.ProtocolTCP}},
				},
			}
			Expect(k8sClient.Create(ctx, oldSvc)).To(Succeed())

			oldCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "httpd-cfg",
					Namespace: oldNamespace,
					Labels:    map[string]string{"app": "gitops-plugin"},
				},
				Data: map[string]string{"test": "data"},
			}
			Expect(k8sClient.Create(ctx, oldCM)).To(Succeed())

			By("verifying the old resources were created successfully")
			Eventually(oldDepl, "30s", "5s").Should(k8sFixture.ExistByName())
			Eventually(oldSvc, "30s", "5s").Should(k8sFixture.ExistByName())
			Eventually(oldCM, "30s", "5s").Should(k8sFixture.ExistByName())

			By("recreating GitopsService CR to trigger reconciliation")
			gitopsService := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: oldNamespace},
			}
			Expect(k8sClient.Create(ctx, gitopsService)).To(Succeed())

			By("verifying old plugin resources are cleaned up from openshift-gitops")
			Eventually(oldDepl, "5m", "5s").Should(k8sFixture.NotExistByName())
			Eventually(oldSvc, "5m", "5s").Should(k8sFixture.NotExistByName())
			Eventually(oldCM, "5m", "5s").Should(k8sFixture.NotExistByName())

			By("verifying plugin resources still exist in the operator namespace")
			newDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: operatorNamespace}}
			Eventually(newDepl, "3m", "5s").Should(k8sFixture.ExistByName())
			Eventually(newDepl, "3m", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			newSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: operatorNamespace}}
			Eventually(newSvc, "60s", "5s").Should(k8sFixture.ExistByName())

			newCM := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "httpd-cfg", Namespace: operatorNamespace}}
			Eventually(newCM, "60s", "5s").Should(k8sFixture.ExistByName())

		})
	})
})
