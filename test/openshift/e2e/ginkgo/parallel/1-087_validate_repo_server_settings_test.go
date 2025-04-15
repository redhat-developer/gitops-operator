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
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-087_validate_repo_server_settings", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("validates that setting mountSAToken and serviceAccount on .spec.repo will cause the values to be set on Repo Server Deployment", func() {
			By("creating simple Argo CD instance")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Server: argov1beta1api.ArgoCDServerSpec{
						Route: argov1beta1api.ArgoCDRouteSpec{
							Enabled: true,
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			By("setting 'mountSAToken: false' and 'ServiceAccount: default' on ArgoCD CR .spec.repo")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Repo.MountSAToken = false
				ac.Spec.Repo.ServiceAccount = "default"
			})

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-repo-server", Namespace: ns.Name}}

			By("verifying expected ArgoCD .spec.repo values are set on Repo server Deployment")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(depl), depl); err != nil {
					GinkgoWriter.Println(err)
					return false
				}

				deplSpecTemplateSpec := depl.Spec.Template.Spec

				if deplSpecTemplateSpec.AutomountServiceAccountToken == nil {
					return false
				}

				GinkgoWriter.Println("Values:", deplSpecTemplateSpec.ServiceAccountName, deplSpecTemplateSpec.DeprecatedServiceAccount)

				return deplSpecTemplateSpec.ServiceAccountName == "default" &&
					*deplSpecTemplateSpec.AutomountServiceAccountToken == false &&
					deplSpecTemplateSpec.DeprecatedServiceAccount == "default"

			}).Should(BeTrue())

			Eventually(depl, "60s", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			serviceAccount := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "modified-default", Namespace: ns.Name}}
			Expect(k8sClient.Create(ctx, serviceAccount)).To(Succeed())

			By("setting different values, 'mountSAToken: true' and 'serviceAccount: modified-default', values on ArgoCD CR .spec.repo")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Repo.MountSAToken = true
				ac.Spec.Repo.ServiceAccount = "modified-default"
			})

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying expected .spec.repo values are set on Repo server Deployment")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(depl), depl); err != nil {
					GinkgoWriter.Println(err)
					return false
				}

				deplSpecTemplateSpec := depl.Spec.Template.Spec

				if deplSpecTemplateSpec.AutomountServiceAccountToken == nil {
					return false
				}

				GinkgoWriter.Println("Values:", deplSpecTemplateSpec.ServiceAccountName, deplSpecTemplateSpec.DeprecatedServiceAccount)

				return deplSpecTemplateSpec.ServiceAccountName == "modified-default" &&
					*deplSpecTemplateSpec.AutomountServiceAccountToken == true &&
					deplSpecTemplateSpec.DeprecatedServiceAccount == "modified-default"

			}).Should(BeTrue())
			Eventually(depl, "60s", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			By("reverting mountSAToken and ServiceAccount values on ArgoCD CR .spec.repo to default")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Repo.MountSAToken = false
				ac.Spec.Repo.ServiceAccount = ""
			})

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying expected .spec.repo values are set on Repo server Deployment")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(depl), depl); err != nil {
					GinkgoWriter.Println(err)
					return false
				}

				deplSpecTemplateSpec := depl.Spec.Template.Spec

				if deplSpecTemplateSpec.AutomountServiceAccountToken == nil {
					return false
				}

				return *deplSpecTemplateSpec.AutomountServiceAccountToken == false

			}).Should(BeTrue())
			Eventually(depl, "60s", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

		})

	})
})
