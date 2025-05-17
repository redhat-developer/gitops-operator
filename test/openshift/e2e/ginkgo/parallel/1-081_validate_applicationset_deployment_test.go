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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-081_validate_applicationset_deployment", func() {

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
		})

		It("verifies that openshift-gitops Argo CD has applicationset controller workload and service with expected values", func() {

			gitopsArgoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())
			Eventually(gitopsArgoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			appsetController := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-applicationset-controller", Namespace: gitopsArgoCD.Namespace}}
			Expect(appsetController).To(k8sFixture.ExistByName())
			Eventually(appsetController).Should(deploymentFixture.HaveReadyReplicas(1))

			Expect(appsetController.Spec.Template.Spec.Containers[0].Ports).To(Equal([]corev1.ContainerPort{
				{ContainerPort: 7000, Name: "webhook", Protocol: "TCP"},
				{ContainerPort: 8080, Name: "metrics", Protocol: "TCP"},
			}))

			appsetService := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-applicationset-controller", Namespace: gitopsArgoCD.Namespace},
			}
			Expect(appsetService).To(k8sFixture.ExistByName())
			Expect(appsetService.Spec.Ports).To(Equal([]corev1.ServicePort{
				{Port: 7000, Name: "webhook", Protocol: "TCP", TargetPort: intstr.FromInt(7000)},
				{Port: 8080, Name: "metrics", Protocol: "TCP", TargetPort: intstr.FromInt(8080)},
			}))

		})

	})
})
