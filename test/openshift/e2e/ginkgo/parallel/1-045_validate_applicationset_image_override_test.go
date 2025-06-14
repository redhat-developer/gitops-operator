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
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-045_validate_applicationset_image_override", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("ensuring that applicationset controller will use custom image specified in .spec.image, and that .spec.applicationset.image will override .spec.image", func() {

			By("creating Argo CD instance with custom image, and applicationset enabled")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Image:   "argocdimage",
					Version: "tag002",
					ApplicationSet: &argov1beta1api.ArgoCDApplicationSet{
						Enabled: ptr.To(true),
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("verifying that applicationset controller uses custom image from field")

			depl := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-argocd-applicationset-controller",
					Namespace: ns.Name,
				},
			}
			Eventually(depl).Should(k8sFixture.ExistByName())

			container := depl.Spec.Template.Spec.Containers[0]
			Expect(container.Image).To(Equal("argocdimage:tag002"))
			Expect(container.Name).To(Equal("argocd-applicationset-controller"))

			By("adding a specific custom image under .spec.applicationset.image, which should override the .spec.image field")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ApplicationSet.Image = "testimage"
				ac.Spec.ApplicationSet.Version = "tag001"
			})

			By("verifying that .spec.applicationset.image overrides .spec.image field")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(depl), depl); err != nil {
					GinkgoWriter.Println(err)
					return false
				}
				container := depl.Spec.Template.Spec.Containers[0]

				return container.Image == "testimage:tag001" && container.Name == "argocd-applicationset-controller"

			}).Should(BeTrue())

		})

	})
})
