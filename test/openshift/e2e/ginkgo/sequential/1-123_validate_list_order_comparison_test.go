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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gitopsoperatorv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	gitopsserviceFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/gitopsservice"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const testReconciliationTriggerAnnotation = "test-reconciliation-trigger"
const gitopsPluginDeploymentName = "gitops-plugin"
const openshiftGitopsNamespace = "openshift-gitops"

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-123_validate_list_order_comparison", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
			runDebug  struct {
				initialGen, genAfterOrderChange, finalGen int64
				expectedImage, lastPluginImage            string
			}
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
			runDebug = struct {
				initialGen, genAfterOrderChange, finalGen int64
				expectedImage, lastPluginImage            string
			}{}
		})

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				GinkgoWriter.Println("++++ 1-123 failure debug start ++++")
				kubeClient, err := fixtureUtils.GetE2ETestKubeClient()
				if err != nil {
					GinkgoWriter.Println(fmt.Sprintf("could not get kube client: %v", err))
				} else {
					c := context.Background()
					gs := &gitopsoperatorv1alpha1.GitopsService{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}}
					gsErr := kubeClient.Get(c, client.ObjectKeyFromObject(gs), gs)
					pluginDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: gitopsPluginDeploymentName, Namespace: openshiftGitopsNamespace}}
					pluginErr := kubeClient.Get(c, client.ObjectKeyFromObject(pluginDepl), pluginDepl)
					readyReplicas, observedGen, deplGen := int32(0), int64(0), int64(0)
					if pluginErr == nil {
						readyReplicas = pluginDepl.Status.ReadyReplicas
						observedGen = pluginDepl.Status.ObservedGeneration
						deplGen = pluginDepl.Generation
					}
					GinkgoWriter.Println(fmt.Sprintf("gs=%v plugin=%v ready=%d gen=%d obs=%d",
						gsErr == nil, pluginErr == nil, readyReplicas, deplGen, observedGen))
					if runDebug.finalGen != 0 || runDebug.genAfterOrderChange != 0 {
						GinkgoWriter.Println(fmt.Sprintf("list-order: initial=%d afterOrder=%d final=%d (want %d)",
							runDebug.initialGen, runDebug.genAfterOrderChange, runDebug.finalGen, runDebug.genAfterOrderChange))
					}
					if runDebug.expectedImage != "" {
						GinkgoWriter.Println(fmt.Sprintf("image: expected=%q last=%q",
							runDebug.expectedImage, runDebug.lastPluginImage))
					}
				}
				GinkgoWriter.Println("++++ 1-123 failure debug end ++++")
			}
			fixture.OutputDebugOnFail(openshiftGitopsNamespace)
		})

		It("Should not trigger updates when only list order differs", func() {
			gitopsService := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gitopsService), gitopsService)).To(Succeed())

			pluginDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gitopsPluginDeploymentName,
					Namespace: openshiftGitopsNamespace,
				},
			}
			Eventually(pluginDeployment, "5m", "5s").Should(k8sFixture.ExistByName(),
				"deployment %s never showed in %s (5m)", gitopsPluginDeploymentName, openshiftGitopsNamespace)
			Eventually(pluginDeployment, "60s", "5s").Should(deploymentFixture.HaveReadyReplicas(1),
				"deployment %s in %s not ready after 60s", gitopsPluginDeploymentName, openshiftGitopsNamespace)

			By("capturing initial state before simulating etcd order change")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDeployment), pluginDeployment)).To(Succeed())
			initialGen := pluginDeployment.Generation
			runDebug.initialGen = initialGen

			hasMultipleContainers := len(pluginDeployment.Spec.Template.Spec.Containers) >= 2
			hasMultipleVolumes := len(pluginDeployment.Spec.Template.Spec.Volumes) >= 2
			hasMultipleTolerations := len(pluginDeployment.Spec.Template.Spec.Tolerations) >= 2

			if !hasMultipleContainers && !hasMultipleVolumes && !hasMultipleTolerations {
				Skip("Deployment does not have multiple containers, volumes, or tolerations to test order differences")
			}

			By("simulating etcd returning lists in different order")
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
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
			runDebug.genAfterOrderChange = genAfterManualOrderChange

			By("triggering reconciliation by updating GitopsService CR")
			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				if gs.Annotations == nil {
					gs.Annotations = make(map[string]string)
				}
				gs.Annotations[testReconciliationTriggerAnnotation] = "list-order-test"
			})
			time.Sleep(10 * time.Second)

			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				if gs.Annotations != nil {
					delete(gs.Annotations, testReconciliationTriggerAnnotation)
				}
			})
			time.Sleep(10 * time.Second)

			By("verifying no unnecessary update was triggered")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDeployment), pluginDeployment)).To(Succeed())
			finalGen := pluginDeployment.Generation
			runDebug.finalGen = finalGen

			Expect(finalGen).To(Equal(genAfterManualOrderChange),
				fmt.Sprintf("Generation should not change when only list order differs. Initial: %d, AfterManualOrderChange: %d, Final: %d", initialGen, genAfterManualOrderChange, finalGen))

		})

		It("Should trigger updates when actual changes are made", func() {
			gitopsService := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gitopsService), gitopsService)).To(Succeed())

			pluginDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gitopsPluginDeploymentName,
					Namespace: openshiftGitopsNamespace,
				},
			}
			Eventually(pluginDeployment, "5m", "5s").Should(k8sFixture.ExistByName(),
				"deployment %s never showed in %s (5m)", gitopsPluginDeploymentName, openshiftGitopsNamespace)
			Eventually(pluginDeployment, "60s", "5s").Should(deploymentFixture.HaveReadyReplicas(1),
				"deployment %s in %s not ready after 60s", gitopsPluginDeploymentName, openshiftGitopsNamespace)

			By("capturing initial state before making actual change")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDeployment), pluginDeployment)).To(Succeed())
			initialGen := pluginDeployment.Generation
			pluginContainer := deploymentFixture.GetTemplateSpecContainerByName(gitopsPluginDeploymentName, *pluginDeployment)
			Expect(pluginContainer).ToNot(BeNil(), "deployment should have container %q", gitopsPluginDeploymentName)
			expectedImage := pluginContainer.Image

			By("making an actual change to the deployment")
			deploymentFixture.Update(pluginDeployment, func(d *appsv1.Deployment) {
				for i := range d.Spec.Template.Spec.Containers {
					if d.Spec.Template.Spec.Containers[i].Name == gitopsPluginDeploymentName {
						d.Spec.Template.Spec.Containers[i].Image = "wrong-image:wrong-tag"
						break
					}
				}
			})

			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDeployment), pluginDeployment)).To(Succeed())
			genAfterChange := pluginDeployment.Generation
			Expect(genAfterChange).ToNot(Equal(initialGen))

			time.Sleep(15 * time.Second)

			By("triggering reconciliation")
			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				if gs.Annotations == nil {
					gs.Annotations = make(map[string]string)
				}
				gs.Annotations[testReconciliationTriggerAnnotation] = "actual-change-test"
			})
			time.Sleep(15 * time.Second)

			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				if gs.Annotations != nil {
					delete(gs.Annotations, testReconciliationTriggerAnnotation)
				}
			})

			By("verifying operator corrected the image back to the expected image")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pluginDeployment), pluginDeployment); err != nil {
					return false
				}
				c := deploymentFixture.GetTemplateSpecContainerByName(gitopsPluginDeploymentName, *pluginDeployment)
				if c == nil {
					return false
				}
				runDebug.lastPluginImage = c.Image
				return c.Image == expectedImage
			}, "5m", "5s").Should(BeTrue(), "Operator should restore the image of container %q to %q within 5m", gitopsPluginDeploymentName, expectedImage)
		})
	})
})
