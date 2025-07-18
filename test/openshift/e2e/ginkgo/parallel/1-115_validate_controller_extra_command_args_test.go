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
	argoprojv1a1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

		It("ensuring extra arguments are deduplicated, replaced, or preserved as expected in application-controller", func() {
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

			// 1: Add new flag
			By("adding a new flag via extraCommandArgs")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Controller.ExtraCommandArgs = []string{"--app-hard-resync", "2"}
			})
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("--app-hard-resync", 0))

			// 2: Replace existing non-repeatable flag
			By("replacing existing default flag with extraCommandArgs")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Controller.ExtraCommandArgs = []string{
					"--status-processors", "15",
					"--kubectl-parallelism-limit", "20",
				}
			})

			// Expect new values to appear
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("--status-processors", 0))
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("15", 0))
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("--kubectl-parallelism-limit", 0))
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("20", 0))

			// Expect default values to be replaced (old default 10 should not appear)
			Consistently(func() bool {
				cmd := appControllerSS.Spec.Template.Spec.Containers[0].Command
				for i := range cmd {
					if cmd[i] == "--status-processors" && i+1 < len(cmd) && cmd[i+1] == "10" {
						return true
					}
				}
				return false
			}).Should(BeFalse())

			// 3: Add duplicate flag+value pairs, which should be ignored
			By("adding duplicate flags with same values")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Controller.ExtraCommandArgs = []string{
					"--status-processors", "15", // duplicate
					"--kubectl-parallelism-limit", "20", // duplicate
					"--hydrator-enabled",
				}
			})
			// Verify --hydrator-enabled gets added
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("--hydrator-enabled", 0))

			// But no duplicate --status-processors or --kubectl-parallelism-limit
			Consistently(func() bool {
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

			// Check that both --metrics-application-labels flags are present
			Eventually(func() bool {
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

var _ = Describe("ArgoCD Resource Health Persist", func() {
	var (
		argoCDName   = "example-argocd"
		appName      = "guestbook"
		appNamespace = "guestbook"
	)

	var (
		k8sClient client.Client
		ctx       context.Context
	)

	BeforeEach(func() {
		fixture.EnsureParallelCleanSlate()

		k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
		ctx = context.Background()

	})

	Context("1-115_validate_resource_health_persisted_in_application_status", func() {
		It("should persist resource health in Application CR status when configured", func() {
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      ns.Name,
				Namespace: ns.Namespace,
			}, ns)
			Expect(err).Should(BeNil())

			By("Creating ArgoCD CR with controller.resource.health.persist=true")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Controller: argov1beta1api.ArgoCDApplicationControllerSpec{},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("Waiting for Application Controller to be ready")
			Eventually(func() bool {
				deploy := &appsv1.StatefulSet{}
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      argoCDName + "-application-controller",
					Namespace: ns.Name,
				}, deploy)
				return err == nil && deploy.Status.ReadyReplicas > 0
			}, "1m", "5s").Should(BeTrue())

			targetNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: appNamespace,
					Labels: map[string]string{
						"argocd.argoproj.io/managed-by": ns.Name,
					},
				},
			}
			By("Creating target namespace for Application")
			Expect(k8sClient.Create(ctx, targetNamespace)).To(Succeed())

			By("Creating ArgoCD Application CR")
			app := &argoprojv1a1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      appName,
					Namespace: ns.Name,
				},
				Spec: argoprojv1a1.ApplicationSpec{
					Project: "default",
					Source: &argoprojv1a1.ApplicationSource{
						RepoURL:        "https://github.com/Rizwana777/argocd-example-apps",
						Path:           "guestbook",
						TargetRevision: "HEAD",
					},
					Destination: argoprojv1a1.ApplicationDestination{
						Server:    "https://kubernetes.default.svc",
						Namespace: targetNamespace.Name,
					},
					SyncPolicy: &argoprojv1a1.SyncPolicy{
						Automated: &argoprojv1a1.SyncPolicyAutomated{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			By("Validating that resource health is persisted in Application CR")
			Eventually(func() bool {
				var fetched argoprojv1a1.Application
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      appName,
					Namespace: ns.Name,
				}, &fetched)
				if err != nil {
					return false
				}

				// Check if application has resources with health information
				if len(fetched.Status.Resources) == 0 {
					return false
				}

				for _, res := range fetched.Status.Resources {
					if res.Health == nil {
						return false
					}
					if res.Health.Status == "" {
						return false
					}
				}

				// Validate resourceHealthSource is NOT present (it is omitted when health is persisted)
				return len(fetched.Status.ResourceHealthSource) == 0
			}, "3m", "5s").Should(BeTrue())

			Expect(k8sClient.Delete(ctx, targetNamespace)).To(Succeed())
		})
	})
})
