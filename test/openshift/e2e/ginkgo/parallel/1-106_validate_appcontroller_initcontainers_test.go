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
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	podFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/pod"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-106_validate_appcontroller_initcontainers", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()

		})

		It("verifies that setting .spec.controller.initContainers on Argo CD CR will cause that init container to be set on application controller StatefulSet", func() {

			By("creating an ArgoCD CR with an init container for application controller")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Controller: argov1beta1api.ArgoCDApplicationControllerSpec{
						InitContainers: []corev1.Container{{
							Name:            "argocd-init",
							Image:           "nginx:latest",
							ImagePullPolicy: corev1.PullAlways,
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("32Mi"),
								},
							},
						}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting the for Argo CD Application Controller Pod to be available")
			appControllerPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller-0", Namespace: ns.Name}}
			Eventually(appControllerPod, "3m", "5s").Should(k8sFixture.ExistByName())

			By("verifying that init container within Pod has the same value that we set in Argo CD CR")
			initContainer := podFixture.GetSpecInitContainerByName("argocd-init", *appControllerPod)
			Expect(initContainer).ToNot(BeNil())

			Expect(initContainer.Image).To(Equal("nginx:latest"))
			Expect(initContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))
			Expect(initContainer.Resources.Limits.Cpu().String()).To(Equal("50m"))
			Expect(initContainer.Resources.Limits.Memory().String()).To(Equal("64Mi"))

			Expect(initContainer.Resources.Requests.Cpu().String()).To(Equal("10m"))
			Expect(initContainer.Resources.Requests.Memory().String()).To(Equal("32Mi"))

			Expect(appControllerPod.Spec.Containers[0].Name).To(Equal("argocd-application-controller"))
		})

	})
})
