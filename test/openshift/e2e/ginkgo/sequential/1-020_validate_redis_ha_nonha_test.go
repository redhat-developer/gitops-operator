/*
Copyright 2021.

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
	"github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/node"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-020_validate_redis_ha_nonha", func() {

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
		})

		It("validates Redis HA and Non-HA", func() {

			// This test enables HA, so it needs to be running on a cluster with at least 3 nodes
			node.ExpectHasAtLeastXNodes(3)

			By("ensuring the openshift-gitops Argo CD instance is running")
			gitopsArgoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())
			Eventually(gitopsArgoCD, "3m", "5s").Should(argocdFixture.BeAvailable())
			Eventually(gitopsArgoCD).Should(argocdFixture.HaveRedisStatus("Running"))

			By("verifying various expected resources exist in namespace")
			Eventually(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis", Namespace: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis", Namespace: "openshift-gitops"}}
			Eventually(depl).Should(k8sFixture.ExistByName())
			Eventually(depl).Should(deploymentFixture.HaveReadyReplicas(1))

			By("verifies Redis HA resources should not exist since we are in non-HA mode")

			Consistently(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis-ha", Namespace: "openshift-gitops"}}).Should(k8sFixture.NotExistByName())

			Consistently(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis-ha-haproxy", Namespace: "openshift-gitops"}}).Should(k8sFixture.NotExistByName())

			Consistently(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis-ha-haproxy", Namespace: "openshift-gitops"}}).Should(k8sFixture.NotExistByName())

			Consistently(&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis-ha-server", Namespace: "openshift-gitops"}}).Should(k8sFixture.NotExistByName())

			By("enabling HA on openshift-gitops Argo CD instance")
			argocdFixture.Update(gitopsArgoCD, func(argocd *v1beta1.ArgoCD) {
				argocd.Spec.HA.Enabled = true
			})

			By("verifying expected HA resources are eventually created after we enabled HA")

			Eventually(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis-ha", Namespace: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

			Eventually(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis-ha-haproxy", Namespace: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

			Eventually(gitopsArgoCD, "4m", "5s").Should(argocdFixture.HavePhase("Available"))
			Eventually(gitopsArgoCD).Should(argocdFixture.HaveRedisStatus("Running"))

			statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis-ha-server", Namespace: "openshift-gitops"}}
			Eventually(statefulSet).Should(statefulsetFixture.HaveReadyReplicas(3))
			Expect(statefulSet.Spec.Template.Spec.Affinity).To(Equal(
				&corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"app.kubernetes.io/name": "openshift-gitops-redis-ha",
									},
								},
								TopologyKey: "kubernetes.io/hostname",
							},
						},
					},
				}))

			Eventually(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis-ha-haproxy", Namespace: "openshift-gitops"}}, "60s", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			By("verifying non-HA resources no longer exist, since HA is enabled")

			Expect(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis", Namespace: "openshift-gitops"}}).To(k8sFixture.NotExistByName())

			Expect(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis", Namespace: "openshift-gitops"}}).To(k8sFixture.NotExistByName())

			By("updating ArgoCD CR to add cpu and memory resource request and limits to HA workloads")

			argocdFixture.Update(gitopsArgoCD, func(argocd *v1beta1.ArgoCD) {
				argocd.Spec.HA.Enabled = true
				argocd.Spec.HA.Resources = &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("200m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				}
			})

			By("Argo CD should eventually be ready after updating the resource requirements")
			Eventually(gitopsArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable()) // it can take a while to schedule the Pods
			Eventually(gitopsArgoCD, "60s", "5s").Should(argocdFixture.HaveRedisStatus("Running"))

			By("verifying Deployment and StatefulSet have expected resources that we set in previous step")

			depl = &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis-ha-haproxy", Namespace: "openshift-gitops"}}
			Eventually(depl, "2m", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			haProxyContainer := deploymentFixture.GetTemplateSpecContainerByName("haproxy", *depl)

			Expect(haProxyContainer).ToNot(BeNil())
			Expect(haProxyContainer.Resources.Limits.Cpu().AsDec().String()).To(Equal("0.500"))
			Expect(haProxyContainer.Resources.Limits.Memory().AsDec().String()).To(Equal("268435456")) // 256Mib in bytes
			Expect(haProxyContainer.Resources.Requests.Cpu().AsDec().String()).To(Equal("0.200"))
			Expect(haProxyContainer.Resources.Requests.Memory().AsDec().String()).To(Equal("134217728")) // 128MiB  in bytes

			configInitContainer := deploymentFixture.GetTemplateSpecInitContainerByName("config-init", *depl)

			Expect(configInitContainer.Resources.Limits.Cpu().AsDec().String()).To(Equal("0.500"))
			Expect(configInitContainer.Resources.Limits.Memory().AsDec().String()).To(Equal("268435456"))
			Expect(configInitContainer.Resources.Requests.Cpu().AsDec().String()).To(Equal("0.200"))
			Expect(configInitContainer.Resources.Requests.Memory().AsDec().String()).To(Equal("134217728"))

			ss := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis-ha-server", Namespace: "openshift-gitops"}}
			Eventually(ss, "2m", "5s").Should(statefulsetFixture.HaveReadyReplicas(3))

			redisContainer := statefulsetFixture.GetTemplateSpecContainerByName("redis", *ss)
			Expect(redisContainer).ToNot(BeNil())
			Expect(redisContainer.Resources.Limits.Cpu().AsDec().String()).To(Equal("0.500"))
			Expect(redisContainer.Resources.Limits.Memory().AsDec().String()).To(Equal("268435456"))
			Expect(redisContainer.Resources.Requests.Cpu().AsDec().String()).To(Equal("0.200"))
			Expect(redisContainer.Resources.Requests.Memory().AsDec().String()).To(Equal("134217728"))

			sentinelContainer := statefulsetFixture.GetTemplateSpecContainerByName("sentinel", *ss)
			Expect(sentinelContainer).ToNot(BeNil())
			Expect(sentinelContainer.Resources.Limits.Cpu().AsDec().String()).To(Equal("0.500"))
			Expect(sentinelContainer.Resources.Limits.Memory().AsDec().String()).To(Equal("268435456"))
			Expect(sentinelContainer.Resources.Requests.Cpu().AsDec().String()).To(Equal("0.200"))
			Expect(sentinelContainer.Resources.Requests.Memory().AsDec().String()).To(Equal("134217728"))

			configInitContainer = statefulsetFixture.GetTemplateSpecInitContainerByName("config-init", *ss)
			Expect(configInitContainer.Resources.Limits.Cpu().AsDec().String()).To(Equal("0.500"))
			Expect(configInitContainer.Resources.Limits.Memory().AsDec().String()).To(Equal("268435456"))
			Expect(configInitContainer.Resources.Requests.Cpu().AsDec().String()).To(Equal("0.200"))
			Expect(configInitContainer.Resources.Requests.Memory().AsDec().String()).To(Equal("134217728"))

			By("disabling HA on ArgoCD CR")

			argocdFixture.Update(gitopsArgoCD, func(argocd *v1beta1.ArgoCD) {
				argocd.Spec.HA.Enabled = false
			})

			By("verifying Argo CD becomes ready again after HA is disabled")

			Eventually(gitopsArgoCD, "60s", "5s").Should(argocdFixture.BeAvailable())
			Eventually(gitopsArgoCD, "60s", "5s").Should(argocdFixture.HaveRedisStatus("Running"))

			By("verifying expected non-HA resources exist again and HA resources no longer exist")
			depl = &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis", Namespace: "openshift-gitops"}}
			Eventually(depl).Should(k8sFixture.ExistByName())
			Eventually(depl).Should(deploymentFixture.HaveReadyReplicas(1))

			Consistently(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis-ha-haproxy", Namespace: "openshift-gitops"}}).Should(k8sFixture.NotExistByName())

			Consistently(&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis-ha-server", Namespace: "openshift-gitops"}}).Should(k8sFixture.NotExistByName())

		})
	})
})
