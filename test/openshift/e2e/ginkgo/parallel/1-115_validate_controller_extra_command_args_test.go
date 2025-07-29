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

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-115_validate_controller_extra_command_args", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()

		})

		It("ensures extra arguments are deduplicated, replaced, or preserved as expected in application-controller", func() {
			By("creating a simple ArgoCD CR and waiting for it to become available")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Controller: argov1beta1api.ArgoCDApplicationControllerSpec{},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			appControllerSS := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-argocd-application-controller",
					Namespace: ns.Name,
				},
			}
			Eventually(appControllerSS).Should(k8sFixture.ExistByName())
			Eventually(appControllerSS).Should(statefulsetFixture.HaveReadyReplicas(1))

			// Verify default values of --status-processors and --kubectl-parallelism-limit
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("--status-processors", 0))
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("20", 0))
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("--kubectl-parallelism-limit", 0))
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("10", 0))

			// 1: Add new flag
			By("adding a new flag via extraCommandArgs")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Controller.ExtraCommandArgs = []string{"--app-hard-resync", "2"}
			})
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("--app-hard-resync", 0))

			// 2: Replace existing non-repeatable flags --status-processors and --kubectl-parallelism-limit
			By("replacing existing default flag with extraCommandArgs")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Controller.ExtraCommandArgs = []string{
					"--status-processors", "15",
					"--kubectl-parallelism-limit", "20",
				}
			})

			By("new values should appear for --status-processors and --kubectl-parallelism-limit")
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("--status-processors", 0))
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("15", 0))
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("--kubectl-parallelism-limit", 0))
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("20", 0))
			Eventually(appControllerSS).ShouldNot(statefulsetFixture.HaveContainerCommandSubstring("--app-hard-resync", 0))

			By("default values should be replaced (old default for --status-processors 20 and --kubectl-parallelism-limit 10 should not appear")
			Consistently(func() bool {
				Expect(k8sClient.Get(ctx, client.ObjectKey{
					Name:      appControllerSS.Name,
					Namespace: appControllerSS.Namespace,
				}, appControllerSS)).To(Succeed())

				cmd := appControllerSS.Spec.Template.Spec.Containers[0].Command
				for i := range cmd {
					if cmd[i] == "--status-processors" && i+1 < len(cmd) && cmd[i+1] == "20" {
						return true
					}
					if cmd[i] == "--kubectl-parallelism-limit" && i+1 < len(cmd) && cmd[i+1] == "10" {
						return true
					}
				}
				return false
			}).Should(BeFalse())

			// 3: Add duplicate flag+value pairs, which should be ignored
			By("adding duplicate flags with same values")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Controller.ExtraCommandArgs = []string{
					"--status-processors", "15",
					"--kubectl-parallelism-limit", "20",
					"--status-processors", "15", // duplicate
					"--kubectl-parallelism-limit", "20", // duplicate
					"--hydrator-enabled",
				}
			})
			// Verify --hydrator-enabled gets added
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("--hydrator-enabled", 0))

			// But no duplicate --status-processors or --kubectl-parallelism-limit
			Consistently(func() bool {
				Expect(k8sClient.Get(ctx, client.ObjectKey{
					Name:      appControllerSS.Name,
					Namespace: appControllerSS.Namespace,
				}, appControllerSS)).To(Succeed())

				cmd := appControllerSS.Spec.Template.Spec.Containers[0].Command

				statusProcessorsCount := 0
				kubectlLimitCount := 0

				for i := 0; i < len(cmd); i++ {
					if cmd[i] == "--status-processors" {
						statusProcessorsCount++
					}
					if cmd[i] == "--kubectl-parallelism-limit" {
						kubectlLimitCount++
					}
				}

				// Fail if either flag appears more than once
				return statusProcessorsCount > 1 || kubectlLimitCount > 1
			}).Should(BeFalse())

			// 4: Add a repeatable flag multiple times with different values
			By("adding a repeatable flag with multiple values")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Controller.ExtraCommandArgs = []string{
					"--metrics-application-labels", "application.argoproj.io/template-version",
					"--metrics-application-labels", "application.argoproj.io/chart-version",
				}
			})

			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("--metrics-application-labels", 0))

			By("Check that both --metrics-application-labels flags are present")
			Eventually(func() bool {
				Expect(k8sClient.Get(ctx, client.ObjectKey{
					Name:      appControllerSS.Name,
					Namespace: appControllerSS.Namespace,
				}, appControllerSS)).To(Succeed())

				cmd := appControllerSS.Spec.Template.Spec.Containers[0].Command

				metricVals := []string{}
				for i := 0; i < len(cmd); i++ {
					if cmd[i] == "--metrics-application-labels" && i+1 < len(cmd) {
						metricVals = append(metricVals, cmd[i+1])
					}
				}

				// Ensure both values are present
				hasMetricLabelTemplate := false
				hasMetricLabelChart := false
				for _, v := range metricVals {
					if v == "application.argoproj.io/template-version" {
						hasMetricLabelTemplate = true
					}
					if v == "application.argoproj.io/chart-version" {
						hasMetricLabelChart = true
					}
				}
				return hasMetricLabelTemplate && hasMetricLabelChart
			}).Should(BeTrue())

			// 5: Remove all extra args
			By("removing all extra args")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Controller.ExtraCommandArgs = nil
			})

			// Expect all custom flags to disappear
			Eventually(appControllerSS).ShouldNot(statefulsetFixture.HaveContainerCommandSubstring("--metrics-application-labels", 0))
			Eventually(appControllerSS).ShouldNot(statefulsetFixture.HaveContainerCommandSubstring("--status-processors 15", 0))
			Eventually(appControllerSS).ShouldNot(statefulsetFixture.HaveContainerCommandSubstring("--kubectl-parallelism-limit 20", 0))
			Eventually(appControllerSS).ShouldNot(statefulsetFixture.HaveContainerCommandSubstring("--hydrator-enabled", 0))
		})
	})
})
