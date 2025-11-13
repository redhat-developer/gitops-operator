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

	rolloutmanagerv1alpha1 "github.com/argoproj-labs/argo-rollouts-manager/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

	Context("1-103_validate_rollouts_imagepullpolicy", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {

			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("creates a cluster-scopes Argo Rollouts instance and verifies the default image pull policy", func() {

			By("creating simple cluster-scoped Argo Rollouts instance via RolloutManager in openshift-gitops namespace")

			rm := &rolloutmanagerv1alpha1.RolloutManager{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-rollout-manager",
					Namespace: "openshift-gitops",
				},
			}
			Expect(k8sClient.Create(ctx, rm)).To(Succeed())

			By("verifying deplyment exists")
			deplName := "argo-rollouts"
			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deplName, Namespace: "openshift-gitops"}}
			Eventually(depl).Should(k8sFixture.ExistByName())
			Eventually(depl, "4m", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			By("verifying deployment has ImagePullPolicy set to default(IfNotPresent)")
			Eventually(deploymentFixture.VerifyDeploymentImagePullPolicy(deplName, "openshift-gitops", corev1.PullIfNotPresent, depl), "3m", "5s").Should(BeTrue(),
				"Deployment %s should have all containers with ImagePullPolicy set to IfNotPresent", deplName)

		})

		It("creates a cluster-scopes Argo Rollouts instance and verifies the CR value imagePullPolicy is applied", func() {

			By("creating simple cluster-scoped Argo Rollouts instance via RolloutManager in openshift-gitops namespace with imagePullPolicy set to Always")

			rm := &rolloutmanagerv1alpha1.RolloutManager{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-rollout-manager",
					Namespace: "openshift-gitops",
				},
				Spec: rolloutmanagerv1alpha1.RolloutManagerSpec{
					ImagePullPolicy: corev1.PullAlways,
				},
			}
			Expect(k8sClient.Create(ctx, rm)).To(Succeed())

			By("verifying deplyment exists")
			deplName := "argo-rollouts"
			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deplName, Namespace: "openshift-gitops"}}
			Eventually(depl).Should(k8sFixture.ExistByName())
			Eventually(depl, "4m", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			By("verifying deployment has ImagePullPolicy set to the CR value(Always)")
			Eventually(deploymentFixture.VerifyDeploymentImagePullPolicy(deplName, "openshift-gitops", corev1.PullAlways, depl), "3m", "5s").Should(BeTrue(),
				"Deployment %s should have all containers with ImagePullPolicy set to Always", deplName)

			By("updating the RolloutManager CR to set imagePullPolicy to Never")
			patch := client.MergeFrom(rm.DeepCopy())
			rm.Spec.ImagePullPolicy = corev1.PullNever
			Expect(k8sClient.Patch(ctx, rm, patch)).To(Succeed())

			By("verifying deployment has ImagePullPolicy set to the CR value(Never)")
			Eventually(deploymentFixture.VerifyDeploymentImagePullPolicy(deplName, "openshift-gitops", corev1.PullNever, depl), "3m", "5s").Should(BeTrue(),
				"Deployment %s should have all containers with ImagePullPolicy set to Never", deplName)

			By("Removing the imagePullPolicy from the CR and check if the deployment has the imagePullPolicy set to default(IfNotPresent)")
			rm.Spec.ImagePullPolicy = ""
			Expect(k8sClient.Patch(ctx, rm, patch)).To(Succeed())

			By("verifying deployment has ImagePullPolicy set to default(IfNotPresent)")
			Eventually(deploymentFixture.VerifyDeploymentImagePullPolicy(deplName, "openshift-gitops", corev1.PullIfNotPresent, depl), "3m", "5s").Should(BeTrue(),
				"Deployment %s should have all containers with ImagePullPolicy set to IfNotPresent", deplName)
		})

		It("creates a cluster-scopes Argo Rollouts instance and verifies subscription image pull policy is applied", func() {
			if fixture.EnvLocalRun() {
				Skip("This test does not support local run, as when the controller is running locally there is no env var to modify")
				return
			}

			By("setting the IMAGE_PULL_POLICY environment variable to Always")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("IMAGE_PULL_POLICY", "Always")
			defer func() {
				By("removing IMAGE_PULL_POLICY environment variable to restore default behavior")
				fixture.RestoreSubcriptionToDefault()
			}()

			By("creating simple cluster-scoped Argo Rollouts instance via RolloutManager in openshift-gitops namespace")
			rm := &rolloutmanagerv1alpha1.RolloutManager{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-rollout-manager",
					Namespace: "openshift-gitops",
				},
			}
			Expect(k8sClient.Create(ctx, rm)).To(Succeed())

			By("verifying deplyment exists")
			deplName := "argo-rollouts"
			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deplName, Namespace: "openshift-gitops"}}
			Eventually(depl).Should(k8sFixture.ExistByName())
			Eventually(depl, "4m", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			By("verifying deployment has ImagePullPolicy set to Always")
			Eventually(deploymentFixture.VerifyDeploymentImagePullPolicy(deplName, "openshift-gitops", corev1.PullAlways, depl), "3m", "5s").Should(BeTrue(),
				"Deployment %s should have all containers with ImagePullPolicy set to Always", deplName)

			By("changing the subscription image pull policy to Never")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("IMAGE_PULL_POLICY", "Never")

			By("verifying deployment has ImagePullPolicy set to Never")
			Eventually(deploymentFixture.VerifyDeploymentImagePullPolicy(deplName, "openshift-gitops", corev1.PullNever, depl), "3m", "5s").Should(BeTrue(),
				"Deployment %s should have all containers with ImagePullPolicy set to Never", deplName)

			By("changing the subscription image pull policy to IfNotPresent")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("IMAGE_PULL_POLICY", "IfNotPresent")

			By("verifying deployment has ImagePullPolicy set to IfNotPresent")
			Eventually(deploymentFixture.VerifyDeploymentImagePullPolicy(deplName, "openshift-gitops", corev1.PullIfNotPresent, depl), "3m", "5s").Should(BeTrue(),
				"Deployment %s should have all containers with ImagePullPolicy set to IfNotPresent", deplName)

			By("setting imagePullPolicy in CR and verify if the deployment has the imagePullPolicy set to the CR value")
			patch := client.MergeFrom(rm.DeepCopy())
			rm.Spec.ImagePullPolicy = corev1.PullAlways
			Expect(k8sClient.Patch(ctx, rm, patch)).To(Succeed())
			Eventually(deploymentFixture.VerifyDeploymentImagePullPolicy(deplName, "openshift-gitops", corev1.PullAlways, depl), "3m", "5s").Should(BeTrue(),
				"Deployment %s should have all containers with ImagePullPolicy set to Always", deplName)

		})

	})

})
