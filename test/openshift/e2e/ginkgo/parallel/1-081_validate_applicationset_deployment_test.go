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
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-081_validate_applicationset_deployment", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that an Argo CD instance has applicationset controller workload and service with expected values", func() {

			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCDName := "argocd-" + string(uuid.NewUUID())[0:8]
			appsetName := argoCDName + "-applicationset-controller"

			By("creating Argo CD CR with ApplicationSet controller enabled")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: argoCDName, Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					ApplicationSet: &argov1beta1api.ArgoCDApplicationSet{},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			appsetController := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: appsetName, Namespace: ns.Name}}
			Eventually(appsetController, "5m", "5s").Should(k8sFixture.ExistByName())
			Eventually(appsetController).Should(deploymentFixture.HaveReadyReplicas(1))

			Expect(appsetController.Spec.Template.Spec.Containers[0].Ports).To(Equal([]corev1.ContainerPort{
				{ContainerPort: 7000, Name: "webhook", Protocol: "TCP"},
				{ContainerPort: 8080, Name: "metrics", Protocol: "TCP"},
			}))

			appsetService := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: appsetName, Namespace: ns.Name},
			}
			Eventually(appsetService).Should(k8sFixture.ExistByName())
			Expect(appsetService.Spec.Ports).To(Equal([]corev1.ServicePort{
				{Port: 7000, Name: "webhook", Protocol: "TCP", TargetPort: intstr.FromInt(7000)},
				{Port: 8080, Name: "metrics", Protocol: "TCP", TargetPort: intstr.FromInt(8080)},
			}))

		})

	})
})
