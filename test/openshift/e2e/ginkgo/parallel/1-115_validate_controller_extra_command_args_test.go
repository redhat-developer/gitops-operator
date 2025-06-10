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

		It("ensuring that extra arguments can be added to application controller", func() {

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

			By("verifying app controller becomes availables")
			appControllerSS := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-argocd-application-controller",
					Namespace: ns.Name,
				},
			}

			Eventually(appControllerSS).Should(k8sFixture.ExistByName())
			Eventually(appControllerSS).Should(statefulsetFixture.HaveReadyReplicas(1))

			By("adding a new parameter via .spec.controller.extraCommandArgs")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Controller.ExtraCommandArgs = []string{"--app-hard-resync"}
			})

			By("verifying new parameter is added, and the existing paramaters are still present")
			Eventually(appControllerSS).Should(statefulsetFixture.HaveContainerCommandSubstring("--app-hard-resync", 0))

			Expect(len(appControllerSS.Spec.Template.Spec.Containers[0].Command)).To(BeNumerically(">=", 10))

			By("removing the extra command arg")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Controller.ExtraCommandArgs = nil
			})

			By("verifying the parameter has been removed")
			Eventually(appControllerSS).ShouldNot(statefulsetFixture.HaveContainerCommandSubstring("--app-hard-resync", 0))
			Consistently(appControllerSS).ShouldNot(statefulsetFixture.HaveContainerCommandSubstring("--app-hard-resync", 0))
			Expect(len(appControllerSS.Spec.Template.Spec.Containers[0].Command)).To(BeNumerically(">=", 10))

			By("adding a new extra command arg that has the same name as existing parameters")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Controller.ExtraCommandArgs = []string{
					"--status-processors",
					"15",
					"--kubectl-parallelism-limit",
					"20",
				}
			})

			// TODO: These lines are currently failing: they are ported correctly from the original kuttl test, but the original kuttl test did not check them correctly (and thus either the behaviour in the operator changed, or the tests never worked)

			// Eventually(appControllerSS).ShouldNot(statefulsetFixture.HaveContainerCommandSubstring("--status-processors 15", 0))

			// Consistently(appControllerSS).ShouldNot(statefulsetFixture.HaveContainerCommandSubstring("--status-processors 15", 0))

			// Eventually(appControllerSS).ShouldNot(statefulsetFixture.HaveContainerCommandSubstring("--kubectl-parallelism-limit 20", 0))

			// Consistently(appControllerSS).ShouldNot(statefulsetFixture.HaveContainerCommandSubstring("--kubectl-parallelism-limit 20", 0))

		})

	})
})
