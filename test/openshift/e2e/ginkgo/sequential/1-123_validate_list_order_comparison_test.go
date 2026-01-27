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
	"time"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const testReconciliationTriggerAnnotation = "test-reconciliation-trigger"

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-123_validate_list_order_comparison", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("Should not trigger updates when only list order differs", func() {
			argocd, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			pluginDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gitops-plugin",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDeployment), pluginDeployment)
			}, "2m", "5s").Should(Succeed())

			By("capturing initial state before simulating etcd order change")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDeployment), pluginDeployment)).To(Succeed())
			initialGen := pluginDeployment.Generation

			hasMultipleContainers := len(pluginDeployment.Spec.Template.Spec.Containers) >= 2
			hasMultipleVolumes := len(pluginDeployment.Spec.Template.Spec.Volumes) >= 2
			hasMultipleTolerations := len(pluginDeployment.Spec.Template.Spec.Tolerations) >= 2

			if !hasMultipleContainers && !hasMultipleVolumes && !hasMultipleTolerations {
				Skip("Deployment does not have multiple containers, volumes, or tolerations to test order differences")
			}

			By("simulating etcd returning lists in different order")
			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDeployment), pluginDeployment); err != nil {
					return err
				}

				if hasMultipleContainers {
					containers := pluginDeployment.Spec.Template.Spec.Containers
					reversed := make([]corev1.Container, len(containers))
					for i := range containers {
						reversed[len(containers)-1-i] = containers[i]
					}
					pluginDeployment.Spec.Template.Spec.Containers = reversed
				}

				if hasMultipleVolumes {
					volumes := pluginDeployment.Spec.Template.Spec.Volumes
					reversed := make([]corev1.Volume, len(volumes))
					for i := range volumes {
						reversed[len(volumes)-1-i] = volumes[i]
					}
					pluginDeployment.Spec.Template.Spec.Volumes = reversed
				}

				if hasMultipleTolerations {
					tolerations := pluginDeployment.Spec.Template.Spec.Tolerations
					reversed := make([]corev1.Toleration, len(tolerations))
					for i := range tolerations {
						reversed[len(tolerations)-1-i] = tolerations[i]
					}
					pluginDeployment.Spec.Template.Spec.Tolerations = reversed
				}

				return k8sClient.Update(ctx, pluginDeployment)
			})
			Expect(err).ToNot(HaveOccurred())

			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDeployment), pluginDeployment)).To(Succeed())
			genAfterManualOrderChange := pluginDeployment.Generation

			By("triggering reconciliation")
			argocdFixture.Update(argocd, func(ac *argov1beta1api.ArgoCD) {
				if ac.Annotations == nil {
					ac.Annotations = make(map[string]string)
				}
				ac.Annotations[testReconciliationTriggerAnnotation] = "list-order-test"
			})
			time.Sleep(10 * time.Second)

			argocdFixture.Update(argocd, func(ac *argov1beta1api.ArgoCD) {
				if ac.Annotations != nil {
					delete(ac.Annotations, testReconciliationTriggerAnnotation)
				}
			})
			time.Sleep(10 * time.Second)

			By("verifying no unnecessary update was triggered")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDeployment), pluginDeployment)).To(Succeed())
			finalGen := pluginDeployment.Generation

			Expect(finalGen).To(Equal(genAfterManualOrderChange),
				fmt.Sprintf("Generation should not change when only list order differs. Initial: %d, AfterManualOrderChange: %d, Final: %d", initialGen, genAfterManualOrderChange, finalGen))

		})

		It("Should trigger updates when actual changes are made", func() {
			argocd, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			pluginDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gitops-plugin",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDeployment), pluginDeployment)
			}, "2m", "5s").Should(Succeed())

			By("capturing initial Generation before making actual change")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDeployment), pluginDeployment)).To(Succeed())
			initialGen := pluginDeployment.Generation

			By("making an actual change to the deployment")
			deploymentFixture.Update(pluginDeployment, func(d *appsv1.Deployment) {
				if len(d.Spec.Template.Spec.Containers) > 0 {
					d.Spec.Template.Spec.Containers[0].Image = "wrong-image:wrong-tag"
				}
			})

			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDeployment), pluginDeployment)).To(Succeed())
			genAfterChange := pluginDeployment.Generation
			Expect(genAfterChange).ToNot(Equal(initialGen))

			By("triggering reconciliation")
			argocdFixture.Update(argocd, func(ac *argov1beta1api.ArgoCD) {
				if ac.Annotations == nil {
					ac.Annotations = make(map[string]string)
				}
				ac.Annotations[testReconciliationTriggerAnnotation] = "actual-change-test"
			})
			time.Sleep(15 * time.Second)

			argocdFixture.Update(argocd, func(ac *argov1beta1api.ArgoCD) {
				if ac.Annotations != nil {
					delete(ac.Annotations, testReconciliationTriggerAnnotation)
				}
			})
			time.Sleep(10 * time.Second)

			By("verifying update was triggered")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDeployment), pluginDeployment)).To(Succeed())
			finalGen := pluginDeployment.Generation

			Expect(finalGen).ToNot(Equal(genAfterChange),
				"Generation should change when actual changes are made",
				initialGen, genAfterChange, finalGen)
		})
	})
})
