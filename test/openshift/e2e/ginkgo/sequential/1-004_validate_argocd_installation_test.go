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
	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"

	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-004_validate_argocd_installation", func() {

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
		})

		It("verifies that default openshift-gitops Argo CD instance becomes available after modifying .spec.controller.processors.operation value", func() {

			By("verifying default openshift-gitops Argo CD instance is available")
			argocd, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())
			Expect(argocd).ToNot(BeNil())

			Eventually(argocd, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("modifying Argo CD instance app controller operation processors to 20")
			argocdFixture.Update(argocd, func(ac *argov1beta1api.ArgoCD) {
				argocd.Spec.Controller.Processors.Operation = 20
			})

			defer func() { // Revert the change
				argocdFixture.Update(argocd, func(ac *argov1beta1api.ArgoCD) {
					argocd.Spec.Controller.Processors = argov1beta1api.ArgoCDApplicationControllerProcessorsSpec{}
				})
			}()

			By("verifying app controller instance becomes available")
			Eventually(argocd, "5m", "5s").Should(argocdFixture.BeAvailable())

		})

	})
})
