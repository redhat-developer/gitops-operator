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

	version "github.com/hashicorp/go-version"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gitopsoperatorv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/common"
	"github.com/redhat-developer/gitops-operator/controllers/util"
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

// ocpVersionLessThanPluginMin returns true when OCP version is below the plugin-reconcile minimum.
// Same check as gitopsservice_controller.go (realMajorVersion < startMajorVersion || (realMajorVersion == startMajorVersion && realMinorVersion < startMinorVersion)).
func ocpVersionLessThanPluginMin(ocpVersion, minVersion string) bool {
	if ocpVersion == "" || minVersion == "" {
		return false
	}
	v1, err := version.NewVersion(ocpVersion)
	if err != nil {
		return false
	}
	v2, err := version.NewVersion(minVersion)
	if err != nil {
		return false
	}
	real := v1.Segments()
	start := v2.Segments()
	if len(real) < 2 || len(start) < 2 {
		return false
	}
	realMajor, realMinor := real[0], real[1]
	startMajor, startMinor := start[0], start[1]
	return realMajor < startMajor || (realMajor == startMajor && realMinor < startMinor)
}

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-123_validate_list_order_comparison", func() {

		var (
			k8sClient     client.Client
			ctx           context.Context
			ocpVersionStr string
			runDebug      struct {
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
			ocpVersionStr, _ = util.GetClusterVersion(k8sClient)
			if ocpVersionLessThanPluginMin(ocpVersionStr, common.DefaultDynamicPluginStartOCPVersion) {
				Skip("Plugin reconciliation is disabled when OCP version < " + common.DefaultDynamicPluginStartOCPVersion + "; skipping 1-123 test")
			}
		})

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				kubeClient, _, err := fixtureUtils.GetE2ETestKubeClientWithError()
				if err == nil {
					pluginDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: gitopsPluginDeploymentName, Namespace: openshiftGitopsNamespace}}
					pluginErr := kubeClient.Get(context.Background(), client.ObjectKeyFromObject(pluginDepl), pluginDepl)
					deplGen, observedGen := int64(0), int64(0)
					if pluginErr == nil {
						deplGen, observedGen = pluginDepl.Generation, pluginDepl.Status.ObservedGeneration
					}
					line := fmt.Sprintf("fail: OCP=%q plugin_reconcile_min=%q plugin_exists=%v gen=%d obs=%d",
						ocpVersionStr, common.DefaultDynamicPluginStartOCPVersion, pluginErr == nil, deplGen, observedGen)
					if runDebug.genAfterOrderChange != 0 || runDebug.finalGen != 0 {
						line += fmt.Sprintf(" list_order: initial=%d afterOrder=%d final=%d",
							runDebug.initialGen, runDebug.genAfterOrderChange, runDebug.finalGen)
					}
					GinkgoWriter.Println(line)
				}
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
			runDebug.expectedImage = expectedImage

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
