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
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-112_validate_server_initcontainers", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()

		})

		It("verifies that an init container can be added to server Deployment via Argo CD CR", func() {

			By("creating an ArgoCD CR containing a server init container")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Server: argov1beta1api.ArgoCDServerSpec{
						InitContainers: []corev1.Container{
							{
								Name:            "argocd-init",
								Image:           "quay.io/nginx/nginx-unprivileged:latest",
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
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("verifying the server Deployment contains that init container")
			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-server", Namespace: ns.Name}}

			Eventually(depl, "4m", "5s").Should(k8sFixture.ExistByName())

			Expect(depl.Spec.Template.Spec.InitContainers).To(HaveLen(1))

			Expect(depl.Spec.Template.Spec.InitContainers[0].Name).To(Equal("argocd-init"))
			Expect(depl.Spec.Template.Spec.InitContainers[0].Image).To(Equal("quay.io/nginx/nginx-unprivileged:latest"))
			Expect(depl.Spec.Template.Spec.InitContainers[0].ImagePullPolicy).To(Equal(corev1.PullAlways))

			Expect(depl.Spec.Template.Spec.InitContainers[0].Resources).To(Equal(
				corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("50m"),
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("10m"),
						corev1.ResourceMemory: resource.MustParse("32Mi"),
					},
				}))

			Expect(depl.Spec.Template.Spec.Containers).To(HaveLen(1))

			Expect(depl.Spec.Template.Spec.Containers[0].Name).To(Equal("argocd-server"))

		})

	})
})
