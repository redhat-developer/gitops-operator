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
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	ssFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-036_validate_keycloak_resource_reqs", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("validates that Keycloak SSO can be enabled", func() {

			By("creating namespace-scoped Argo CD instance")

			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			expectEverythingIsRunning := func() {
				By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
				Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

				By("ensuring all expected Deployments and StatefulSets are running")
				deploymentsShouldExist := []string{"argocd-redis", "argocd-server", "argocd-repo-server"}
				for _, depl := range deploymentsShouldExist {
					depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: depl, Namespace: ns.Name}}
					Eventually(depl).Should(k8sFixture.ExistByName())
					Eventually(depl).Should(deplFixture.HaveReplicas(1))
					Eventually(depl).Should(deplFixture.HaveReadyReplicas(1))
				}

				statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: ns.Name}}
				Eventually(statefulSet).Should(k8sFixture.ExistByName())
				Eventually(statefulSet).Should(ssFixture.HaveReplicas(1))
				Eventually(statefulSet).Should(ssFixture.HaveReadyReplicas(1))

			}

			expectEverythingIsRunning()

			By("set .spec.SSO.provider = keycloak")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.SSO = &argov1beta1api.ArgoCDSSOSpec{
					Provider: "keycloak",
				}
			})

			By("verifying keycloak-1-deploy pod should be created")
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "keycloak-1-deploy", Namespace: ns.Name}}
			Eventually(pod).Should(k8sFixture.ExistByName())

			By("verifying keycloak-1-deploy pod should successfully complete")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), pod); err != nil {
					GinkgoWriter.Println(err)
					return false
				}

				matchFound := false
				for _, item := range pod.Status.ContainerStatuses {
					if item.Name == "deployment" {
						GinkgoWriter.Println("Pod keycloak-1-deploy status has terminated status:", item.State.Terminated)
						if item.State.Terminated != nil && item.State.Terminated.Reason == "Completed" {
							matchFound = true
						}
					}
				}
				return matchFound

			}, "4m", "2s").Should(BeTrue())

			By("verifying all other Argo CD components should be successfully running")
			expectEverythingIsRunning()

			By("verifying keycloak-1-deploy has expected requests/limits")
			pod = &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "keycloak-1-deploy", Namespace: ns.Name}}
			Eventually(pod).Should(k8sFixture.ExistByName())

			podContainerResources := pod.Spec.Containers[0].Resources

			Expect(podContainerResources.Limits.Cpu().AsDec().String()).To(Equal("0.500"))
			Expect(podContainerResources.Limits.Memory().AsDec().String()).To(Equal("536870912")) // 512MiB
			Expect(podContainerResources.Requests.Cpu().AsDec().String()).To(Equal("0.250"))
			Expect(podContainerResources.Requests.Memory().AsDec().String()).To(Equal("268435456")) // 256MiB

		})

	})
})
