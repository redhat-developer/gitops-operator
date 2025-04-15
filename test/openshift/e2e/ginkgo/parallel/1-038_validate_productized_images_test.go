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
	"fmt"
	"strings"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-038_validate_productized_images", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("validates that Argo CD components are based on registry.redhat.io images", func() {

			if fixture.EnvNonOLM() {
				Skip("skipping test as NON_OLM env is set. This test requires registry.redhat.io images, which are installed via OLM")
				return
			}

			if fixture.EnvLocalRun() {
				Skip("skipping test as LOCAL_RUN env is set. This test requires registry.redhat.io images, which are installed via OLM")
				return
			}

			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			expectPodHasRedHatImage := func(name string, template corev1.PodTemplateSpec) {

				By("verifying the Deployment/StatefulSet " + name + " has a registry.redhat.io image")
				for _, container := range template.Spec.Containers {

					if !strings.Contains(container.Image, "registry.redhat.io/openshift-gitops-1/argocd-rhel") {
						msg := fmt.Sprintln("Non-productized image in workload", name, "detected.")

						if !fixture.EnvCI() {
							Fail(msg)
						} else {
							GinkgoWriter.Println(msg)
						}
					}
				}
			}

			deployments := []string{"argocd-server", "argocd-repo-server"}
			for _, deployment := range deployments {
				depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deployment, Namespace: ns.Name}}
				Eventually(depl).Should(k8sFixture.ExistByName())
				expectPodHasRedHatImage(depl.Name, depl.Spec.Template)
			}

			statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: ns.Name}}
			Eventually(statefulSet).Should(k8sFixture.ExistByName())
			expectPodHasRedHatImage(statefulSet.Name, statefulSet.Spec.Template)

		})

	})
})
