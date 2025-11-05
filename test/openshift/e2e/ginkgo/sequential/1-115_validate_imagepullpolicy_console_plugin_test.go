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

package sequential

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gitopsoperatorv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
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
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("validates ImagePullPolicy propagation from GitOpsService CR to console plugin and backend deployments", func() {
			By("verifying Argo CD in openshift-gitops exists and is available")
			if fixture.EnvNonOLM() {
				Skip("Skipping test as NON_OLM env var is set. This test requires operator to running via CSV.")
				return
			}

			if fixture.EnvLocalRun() {
				Skip("Skipping test as LOCAL_RUN env var is set. There is no CSV to modify in this case.")
				return
			}

			argoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			Eventually(argoCD).Should(k8sFixture.ExistByName())
			Eventually(argoCD).Should(argocdFixture.BeAvailable())

			csv := getCSV(ctx, k8sClient)
			Expect(csv).ToNot(BeNil())
			defer func() { Expect(fixture.RemoveDynamicPluginFromCSV(ctx, k8sClient)).To(Succeed()) }()

			ocVersion := getOCPVersion()
			Expect(ocVersion).ToNot(BeEmpty())
			if strings.Contains(ocVersion, "4.15.") {
				Skip("skipping this test as OCP version is 4.15")
				return
			}
			addDynamicPluginEnv(csv, ocVersion)

			By("getting the cluster-scoped GitOpsService CR")
			gitopsService := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: argoCD.Namespace},
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gitopsService), gitopsService)).To(Succeed())

			By("setting ImagePullPolicy to Always in GitOpsService CR")
			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				gs.Spec.ImagePullPolicy = corev1.PullAlways
			})

			By("verifying console plugin deployment has ImagePullPolicy set to Always")
			pluginDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: argoCD.Namespace}}
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

			By("verifying backend deployment has ImagePullPolicy set to Always")
			clusterDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: argoCD.Namespace}}
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
		})

		It("validates default ImagePullPolicy when not set in CR", func() {
			By("verifying Argo CD in openshift-gitops exists and is available")
			if fixture.EnvNonOLM() {
				Skip("Skipping test as NON_OLM env var is set. This test requires operator to running via CSV.")
				return
			}

			if fixture.EnvLocalRun() {
				Skip("Skipping test as LOCAL_RUN env var is set. There is no CSV to modify in this case.")
				return
			}

			argoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			Eventually(argoCD).Should(k8sFixture.ExistByName())
			Eventually(argoCD).Should(argocdFixture.BeAvailable())

			csv := getCSV(ctx, k8sClient)
			Expect(csv).ToNot(BeNil())
			defer func() { Expect(fixture.RemoveDynamicPluginFromCSV(ctx, k8sClient)).To(Succeed()) }()

			ocVersion := getOCPVersion()
			Expect(ocVersion).ToNot(BeEmpty())
			if strings.Contains(ocVersion, "4.15.") {
				Skip("skipping this test as OCP version is 4.15")
				return
			}
			addDynamicPluginEnv(csv, ocVersion)

			By("getting the GitOpsService CR")
			gitopsService := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gitopsService), gitopsService)).To(Succeed())

			By("verifying backend deployment defaults to IfNotPresent")
			clusterDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: argoCD.Namespace}}
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

			By("verifying plugin deployment defaults to IfNotPresent")
			pluginDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: argoCD.Namespace}}
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

		It("validates ImagePullPolicy set as env variable in subscription", func() {
			if fixture.EnvLocalRun() {
				Skip("This test does not support local run, as when the controller is running locally there is no env var to modify")
				return
			}

			csv := getCSV(ctx, k8sClient)
			Expect(csv).ToNot(BeNil())
			defer func() { Expect(fixture.RemoveDynamicPluginFromCSV(ctx, k8sClient)).To(Succeed()) }()

			ocVersion := getOCPVersion()
			Expect(ocVersion).ToNot(BeEmpty())
			if strings.Contains(ocVersion, "4.15.") {
				Skip("skipping this test as OCP version is 4.15")
				return
			}
			addDynamicPluginEnv(csv, ocVersion)

			By("adding image pull policy env variable IMAGE_PULL_POLICY in Subscription")

			fixture.SetEnvInOperatorSubscriptionOrDeployment("IMAGE_PULL_POLICY", "Always")
			defer func() {
				By("removing IMAGE_PULL_POLICY environment variable to restore default behavior")
				fixture.RestoreSubcriptionToDefault()
			}()

			By("verifying Argo CD in openshift-gitops exists and is available")
			argoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			Eventually(argoCD).Should(k8sFixture.ExistByName())
			Eventually(argoCD).Should(argocdFixture.BeAvailable())

			By("getting the GitOpsService CR")
			gitopsService := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gitopsService), gitopsService)).To(Succeed())

			By("printing deployment ImagePullPolicy")
			deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "openshift-gitops"}}
			for _, container := range deployment.Spec.Template.Spec.Containers {
				fmt.Println("Container: " + container.Name + " is " + string(container.ImagePullPolicy))
			}

			By("verifying backend deployment has ImagePullPolicy set based on env variable")
			clusterDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: argoCD.Namespace}}
			Eventually(clusterDepl).Should(k8sFixture.ExistByName())
			fmt.Println("Printing the list of deployment env variables")
			envList := clusterDepl.Spec.Template.Spec.Containers[0].Env
			for _, env := range envList {
				fmt.Println("Env: " + env.Name + " is " + env.Value)
			}

			envValue, err := fixture.GetEnvInOperatorSubscriptionOrDeployment("IMAGE_PULL_POLICY")
			Expect(err).ToNot(HaveOccurred())
			fmt.Println("EnvValue: " + string(*envValue))

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(clusterDepl), clusterDepl)
				if err != nil {
					return false
				}
				for _, container := range clusterDepl.Spec.Template.Spec.Containers {
					if container.ImagePullPolicy != corev1.PullAlways {
						fmt.Println("ImagePullPolicy is set to " + string(container.ImagePullPolicy))
						return false
					}
				}
				return true
			}, "5m", "5s").Should(BeTrue())

			By("verifying plugin deployment has ImagePullPolicy set based on env variable")
			pluginDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: argoCD.Namespace}}
			Eventually(pluginDepl).Should(k8sFixture.ExistByName())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDepl), pluginDepl)
				if err != nil {
					return false
				}
				for _, container := range pluginDepl.Spec.Template.Spec.Containers {
					if container.ImagePullPolicy != corev1.PullAlways {
						fmt.Println("ImagePullPolicy is set to " + container.ImagePullPolicy)
						return false
					}
				}
				return true
			}, "3m", "5s").Should(BeTrue())

			By("updating image pull policy env variable to Never")

			fixture.SetEnvInOperatorSubscriptionOrDeployment("IMAGE_PULL_POLICY", "Never")
			defer func() {
				By("removing IMAGE_PULL_POLICY environment variable to restore default behavior")
				fixture.RestoreSubcriptionToDefault()
			}()

			By("verifying backend deployment has ImagePullPolicy changed based on env variable")
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

			By("verifying plugin deployment has ImagePullPolicy changed based on env variable")

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

			fixture.SetEnvInOperatorSubscriptionOrDeployment("IMAGE_PULL_POLICY", "IfNotPresent")
			defer func() {
				By("removing IMAGE_PULL_POLICY environment variable to restore default behavior")
				fixture.RestoreSubcriptionToDefault()
			}()

			By("verifying backend deployment has ImagePullPolicy changed based on env variable")
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

			By("verifying plugin deployment has ImagePullPolicy changed based on env variable")

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
