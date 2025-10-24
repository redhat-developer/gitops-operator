/*
Copyright 2025.

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

package parallel

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gitopsoperatorv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	gitopsserviceFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/gitopsservice"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-115-validate_imagepullpolicy_gitopsservice", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("validates ImagePullPolicy propagation from GitOpsService CR to console plugin and backend deployments", func() {

			By("getting the cluster-scoped GitOpsService CR")
			gitopsService := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gitopsService), gitopsService)).To(Succeed())

			By("setting ImagePullPolicy to Always in GitOpsService CR")
			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				gs.Spec.ImagePullPolicy = corev1.PullAlways
			})

			By("verifying console plugin deployment has ImagePullPolicy set to Always")
			pluginDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: "openshift-gitops"}}
			Eventually(pluginDepl).Should(k8sFixture.ExistByName())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDepl), pluginDepl)
				if err != nil {
					return false
				}
				for _, container := range pluginDepl.Spec.Template.Spec.Containers {
					if container.ImagePullPolicy != corev1.PullAlways {
						return false
					}
				}
				return true
			}, "3m", "5s").Should(BeTrue())
			//Eventually(pluginDepl, "3m", "5s").Should(deploymentFixture.HaveContainerImagePullPolicy(0, corev1.PullAlways))

			By("verifying backend deployment has ImagePullPolicy set to Always")
			clusterDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "openshift-gitops"}}
			Eventually(clusterDepl).Should(k8sFixture.ExistByName())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(clusterDepl), clusterDepl)
				if err != nil {
					return false
				}
				for _, container := range clusterDepl.Spec.Template.Spec.Containers {
					if container.ImagePullPolicy != corev1.PullAlways {
						return false
					}
				}
				return true
			}, "3m", "5s").Should(BeTrue())
			//Eventually(clusterDepl, "3m", "5s").Should(deploymentFixture.HaveContainerImagePullPolicy(0, corev1.PullAlways))

			By("setting ImagePullPolicy to Never in GitOpsService CR")
			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				gs.Spec.ImagePullPolicy = corev1.PullNever
			})

			By("verifying console plugin deployment has ImagePullPolicy set to Never")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDepl), pluginDepl)
				if err != nil {
					return false
				}
				for _, container := range pluginDepl.Spec.Template.Spec.Containers {
					if container.ImagePullPolicy != corev1.PullNever {
						return false
					}
				}
				return true
			}, "3m", "5s").Should(BeTrue())
			//Eventually(pluginDepl, "3m", "5s").Should(deploymentFixture.HaveContainerImagePullPolicy(0, corev1.PullNever))

			By("verifying backend deployment has ImagePullPolicy set to Never")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(clusterDepl), clusterDepl)
				if err != nil {
					return false
				}
				for _, container := range clusterDepl.Spec.Template.Spec.Containers {
					if container.ImagePullPolicy != corev1.PullNever {
						return false
					}
				}
				return true
			}, "3m", "5s").Should(BeTrue())
			//Eventually(clusterDepl, "3m", "5s").Should(deploymentFixture.HaveContainerImagePullPolicy(0, corev1.PullNever))

			By("setting ImagePullPolicy to IfNotPresent in GitOpsService CR")
			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				gs.Spec.ImagePullPolicy = corev1.PullIfNotPresent
			})

			By("verifying console plugin deployment has ImagePullPolicy set to IfNotPresent")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDepl), pluginDepl)
				if err != nil {
					return false
				}
				for _, container := range pluginDepl.Spec.Template.Spec.Containers {
					if container.ImagePullPolicy != corev1.PullIfNotPresent {
						return false
					}
				}
				return true
			}, "3m", "5s").Should(BeTrue())
			//Eventually(pluginDepl, "3m", "5s").Should(deploymentFixture.HaveContainerImagePullPolicy(0, corev1.PullIfNotPresent))

			By("verifying backend deployment has ImagePullPolicy set to IfNotPresent")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(clusterDepl), clusterDepl)
				if err != nil {
					return false
				}
				for _, container := range clusterDepl.Spec.Template.Spec.Containers {
					if container.ImagePullPolicy != corev1.PullIfNotPresent {
						return false
					}
				}
				return true
			}, "3m", "5s").Should(BeTrue())
			//Eventually(clusterDepl, "3m", "5s").Should(deploymentFixture.HaveContainerImagePullPolicy(0, corev1.PullIfNotPresent))
		})

		It("validates default ImagePullPolicy when not set in CR", func() {
			By("getting the GitOpsService CR")
			gitopsService := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gitopsService), gitopsService)).To(Succeed())

			By("verifying backend deployment defaults to IfNotPresent")
			clusterDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "openshift-gitops"}}
			Eventually(clusterDepl).Should(k8sFixture.ExistByName())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(clusterDepl), clusterDepl)
				if err != nil {
					return false
				}
				for _, container := range clusterDepl.Spec.Template.Spec.Containers {
					if container.ImagePullPolicy != corev1.PullIfNotPresent {
						return false
					}
				}
				return true
			}, "3m", "5s").Should(BeTrue())
			//Eventually(clusterDepl.Spec.Template.Spec.Containers[0].ImagePullPolicy == corev1.PullIfNotPresent, "60s", "3s").Should(BeTrue())
			//deploymentFixture.HaveContainerImagePullPolicy(0, corev1.PullIfNotPresent))

			By("verifying plugin deployment defaults to IfNotPresent")
			pluginDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: "openshift-gitops"}}
			Eventually(pluginDepl).Should(k8sFixture.ExistByName())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDepl), pluginDepl)
				if err != nil {
					return false
				}
				for _, container := range pluginDepl.Spec.Template.Spec.Containers {
					if container.ImagePullPolicy != corev1.PullIfNotPresent {
						return false
					}
				}
				return true
			}, "3m", "5s").Should(BeTrue())
		})
	})
})
