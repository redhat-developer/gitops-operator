/*
Copyright 2021.

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
	deplFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-037_validate_argocd_setting_replicas", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that setting repo/server replicas in ArgoCD .spec will set them in repo/server Deployment", func() {
			By("creating namespace-scoped Argo CD instance")

			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			By("updating server and repo replicas to 3")

			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Server.Replicas = ptr.To((int32)(3))
			})

			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Repo.Replicas = ptr.To((int32)(3))
			})

			By("verifying 3 server and repo replicas become ready")
			serverDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-server", Namespace: ns.Name}}
			Eventually(serverDepl, "60s", "1s").Should(deplFixture.HaveReadyReplicas(3))

			repoDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-repo-server", Namespace: ns.Name}}
			Eventually(repoDepl, "60s", "1s").Should(deplFixture.HaveReadyReplicas(3))

			By("updating server and repo replicas to 1")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Server.Replicas = ptr.To((int32)(1))
			})

			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Repo.Replicas = ptr.To((int32)(1))
			})

			By("verifying 1 server and repo replicas become ready")
			Eventually(serverDepl, "60s", "1s").Should(deplFixture.HaveReadyReplicas(1))

			Eventually(repoDepl, "60s", "1s").Should(deplFixture.HaveReadyReplicas(1))

		})
	})
})
