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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-096-validate_home_env_argocd_controller", func() {

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

		})

		It("verifies openshift-gitops app controller StatefulSet container has expected HOME and REDIS_PASSWORD values", func() {

			By("verifying openshift-gitops-application-controller StatefulSet has the expected values for HOME and REDIS_PASSWORD")
			ss := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-application-controller",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(ss).Should(k8sFixture.ExistByName())

			Expect(ss).Should(statefulset.HaveContainerWithEnvVar("HOME", "/home/argocd", 0))

			container := ss.Spec.Template.Spec.Containers[0]
			match := false

			for _, env := range container.Env {
				if env.Name != "REDIS_PASSWORD" {
					continue
				}

				Expect(env).To(Equal(corev1.EnvVar{
					Name: "REDIS_PASSWORD",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "openshift-gitops-redis-initial-password",
							},
							Key: "admin.password",
						},
					},
				}))
				match = true
			}

			Expect(match).To(BeTrue())

		})

	})
})
