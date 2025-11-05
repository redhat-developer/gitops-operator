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

	rolloutmanagerv1alpha1 "github.com/argoproj-labs/argo-rollouts-manager/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-121_validate_custom_labels_rollouts", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("ensures that custom labels set by the operator are added to Argo Rollouts resources", func() {

			By("creating namespace-scoped RolloutManager instance")
			rolloutManager := &rolloutmanagerv1alpha1.RolloutManager{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rollout-manager",
					Namespace: "openshift-gitops",
				},
			}
			Expect(k8sClient.Create(ctx, rolloutManager)).To(Succeed())

			By("waiting for Argo Rollouts deployment to be created and become available")
			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argo-rollouts", Namespace: "openshift-gitops"}}
			Eventually(depl, "2m", "2s").Should(k8sFixture.ExistByName())

			By("verifying Argo Rollouts Secret has the custom labels")
			labelKey := "operator.argoproj.io/tracked-by"
			labelValue := "argocd"
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "argo-rollouts-notification-secret", Namespace: "openshift-gitops"}}
			Eventually(secret, "2m", "1s").Should(k8sFixture.ExistByName())
			Expect(secret.Labels).Should(HaveKeyWithValue(labelKey, labelValue))

			By("verifying Argo Rollouts ConfigMap has the custom labels")
			configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argo-rollouts-config", Namespace: "openshift-gitops"}}
			Eventually(configMap, "2m", "1s").Should(k8sFixture.ExistByName())
			Expect(configMap.Labels).Should(HaveKeyWithValue(labelKey, labelValue))

			By("updating RolloutManager spec")

			// Add a new label to trigger reconciliation
			if rolloutManager.ObjectMeta.Labels == nil {
				rolloutManager.ObjectMeta.Labels = make(map[string]string)
			}
			rolloutManager.ObjectMeta.Labels["test-update"] = "true"
			patch := client.MergeFrom(rolloutManager.DeepCopy())
			Expect(k8sClient.Patch(ctx, rolloutManager, patch)).To(Succeed())

			By("ensures that custom labels persist in configmap after RolloutManager updates")
			configMap = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argo-rollouts-config", Namespace: "openshift-gitops"}}
			Eventually(configMap, "2m", "1s").Should(k8sFixture.ExistByName())
			Expect(configMap.Labels).Should(HaveKeyWithValue(labelKey, labelValue))

			By("ensures that custom labels persist in secret after RolloutManager updates")
			secret = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "argo-rollouts-notification-secret", Namespace: "openshift-gitops"}}
			Eventually(secret, "2m", "1s").Should(k8sFixture.ExistByName())
			Expect(secret.Labels).Should(HaveKeyWithValue(labelKey, labelValue))
		})

	})
})
