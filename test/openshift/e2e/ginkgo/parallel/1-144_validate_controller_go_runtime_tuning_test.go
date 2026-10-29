/*
Copyright 2026.

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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	argov1beta1api "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-144_validate_controller_go_runtime_tuning", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that GOMEMLIMIT is automatically derived from the application controller's memory limit, and that a user-specified value in .spec.controller.env always takes precedence", func() {

			By("creating a namespace-scoped Argo CD instance with a memory limit configured on the controller")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Controller: argov1beta1api.ArgoCDApplicationControllerSpec{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("250m"),
								corev1.ResourceMemory: resource.MustParse("500Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("1"),
								// 2000Mi limit -> 80% GOMEMLIMIT default = 1677721600 bytes
								corev1.ResourceMemory: resource.MustParse("2000Mi"),
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			ss := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: ns.Name}}
			Eventually(ss).Should(k8sFixture.ExistByName())

			By("verifying that GOMEMLIMIT was automatically set on the application controller StatefulSet, based on the configured memory limit")
			Eventually(ss, "60s", "5s").Should(statefulsetFixture.HaveContainerWithEnvVar("GOMEMLIMIT", "1677721600", 0))

			By("setting a custom GOMEMLIMIT value via .spec.controller.env")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Controller.Env = []corev1.EnvVar{{Name: "GOMEMLIMIT", Value: "999999999"}}
			})

			By("verifying that the user-specified GOMEMLIMIT value always takes precedence over the automatically derived value")
			Eventually(ss, "60s", "5s").Should(statefulsetFixture.HaveContainerWithEnvVar("GOMEMLIMIT", "999999999", 0))
		})

		It("verifies that .spec.controller.goRuntime allows per-instance opt-out and memory percentage overrides", func() {

			By("creating a namespace-scoped Argo CD instance with a memory limit configured on the controller")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Controller: argov1beta1api.ArgoCDApplicationControllerSpec{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: corev1.ResourceList{
								// 2000Mi limit -> 80% default GOMEMLIMIT = 1677721600 bytes
								corev1.ResourceMemory: resource.MustParse("2000Mi"),
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			ss := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: ns.Name}}
			Eventually(ss).Should(k8sFixture.ExistByName())

			By("verifying that GOMEMLIMIT is set by default, derived from the memory limit")
			Eventually(ss, "60s", "5s").Should(statefulsetFixture.HaveContainerWithEnvVar("GOMEMLIMIT", "1677721600", 0))

			By("setting .spec.controller.goRuntime.memoryLimitPercent to a custom value")
			falseVal := false
			var fiftyPercent int32 = 50
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Controller.GoRuntime = &argov1beta1api.ArgoCDGoRuntimeSpec{
					MemoryLimitPercent: &fiftyPercent,
				}
			})

			By("verifying that GOMEMLIMIT is recomputed using the custom memory percentage")
			Eventually(ss, "60s", "5s").Should(statefulsetFixture.HaveContainerWithEnvVar("GOMEMLIMIT", "1048576000", 0)) // 2000Mi * 50 / 100

			By("setting .spec.controller.goRuntime.enabled to false to opt out of Go runtime tuning for this instance")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Controller.GoRuntime = &argov1beta1api.ArgoCDGoRuntimeSpec{
					Enabled: &falseVal,
				}
			})

			By("verifying that GOMEMLIMIT is no longer present on the application-controller container")
			Eventually(func() bool {
				Expect(k8sClient.Get(ctx, client.ObjectKey{
					Name:      ss.Name,
					Namespace: ss.Namespace,
				}, ss)).To(Succeed())

				for _, e := range ss.Spec.Template.Spec.Containers[0].Env {
					if e.Name == "GOMEMLIMIT" {
						return false
					}
				}
				return true
			}, "60s", "5s").Should(BeTrue())
		})

		It("verifies that GOMAXPROCS is automatically derived from the CPU limit alongside GOMEMLIMIT", func() {

			By("creating a namespace-scoped Argo CD instance with CPU and memory limits configured on the controller")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Controller: argov1beta1api.ArgoCDApplicationControllerSpec{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("2000m"), // 2 cores
								corev1.ResourceMemory: resource.MustParse("2000Mi"),
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			ss := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: ns.Name}}
			Eventually(ss).Should(k8sFixture.ExistByName())

			By("verifying that GOMAXPROCS is set by default, derived from the CPU limit")
			Eventually(ss, "60s", "5s").Should(statefulsetFixture.HaveContainerWithEnvVar("GOMAXPROCS", "2", 0))

			By("verifying that GOMEMLIMIT is also set (both derivations work together)")
			Eventually(ss, "60s", "5s").Should(statefulsetFixture.HaveContainerWithEnvVar("GOMEMLIMIT", "1677721600", 0))
		})

		It("verifies that GOMAXPROCS is floored at 1 for sub-1-core CPU limits", func() {

			By("creating a namespace-scoped Argo CD instance with a sub-1-core CPU limit")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Controller: argov1beta1api.ArgoCDApplicationControllerSpec{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("250m"), // 0.25 cores
								corev1.ResourceMemory: resource.MustParse("2000Mi"),
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			ss := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: ns.Name}}
			Eventually(ss).Should(k8sFixture.ExistByName())

			By("verifying that GOMAXPROCS is set to 1 (floored minimum), not 0")
			Eventually(ss, "60s", "5s").Should(statefulsetFixture.HaveContainerWithEnvVar("GOMAXPROCS", "1", 0))
		})

	})
})
