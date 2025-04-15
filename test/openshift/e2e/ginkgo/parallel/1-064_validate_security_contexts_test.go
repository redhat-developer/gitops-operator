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
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-064_validate_security_contexts", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that various Argo CD component workloads have expected security context", func() {

			By("creating simple namespace-scoped Argo CD instance")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					ApplicationSet: &argov1beta1api.ArgoCDApplicationSet{
						Resources: &corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("2"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("250m"),
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
						},
					},
					Notifications: argov1beta1api.ArgoCDNotifications{
						Enabled: true,
					},
					SSO: &argov1beta1api.ArgoCDSSOSpec{
						Provider: argov1beta1api.SSOProviderTypeDex,
						Dex: &argov1beta1api.ArgoCDDexSpec{
							OpenShiftOAuth: true,
							Resources: &corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("250m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying that each Argo CD deployment has expected security context")

			deployments := []string{"argocd-applicationset-controller", "argocd-dex-server", "argocd-notifications-controller", "argocd-redis", "argocd-repo-server", "argocd-server"}
			for _, deployment := range deployments {

				depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deployment, Namespace: ns.Name}}
				Eventually(depl).Should(k8sFixture.ExistByName())

				By("verifying " + depl.Name)

				containers := depl.Spec.Template.Spec.Containers
				Expect(containers).To(HaveLen(1))

				container := containers[0]
				secContext := container.SecurityContext
				Expect(secContext).ToNot(BeNil())

				if depl.Name == "argocd-applicationset-controller" {
					Expect(*secContext).To(Equal(corev1.SecurityContext{
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						AllowPrivilegeEscalation: ptr.To(false),
						ReadOnlyRootFilesystem:   ptr.To(true),
						RunAsNonRoot:             ptr.To(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type:             corev1.SeccompProfileTypeRuntimeDefault,
							LocalhostProfile: nil,
						},
					}))
				} else if depl.Name == "argocd-dex-server" {
					Expect(*secContext).To(Equal(corev1.SecurityContext{
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						AllowPrivilegeEscalation: ptr.To(false),
						RunAsNonRoot:             ptr.To(true),
						ReadOnlyRootFilesystem:   ptr.To(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type:             corev1.SeccompProfileTypeRuntimeDefault,
							LocalhostProfile: nil,
						},
					}))
				} else if depl.Name == "argocd-notifications-controller" {
					Expect(*secContext).To(Equal(corev1.SecurityContext{
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						AllowPrivilegeEscalation: ptr.To(false),
						RunAsNonRoot:             ptr.To(true),
						ReadOnlyRootFilesystem:   ptr.To(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type:             corev1.SeccompProfileTypeRuntimeDefault,
							LocalhostProfile: nil,
						},
					}))

					Expect(depl.Spec.Template.Spec.SecurityContext.RunAsNonRoot).To(Equal(ptr.To(true)))

				} else if depl.Name == "argocd-redis" {
					Expect(*secContext).To(Equal(corev1.SecurityContext{
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						AllowPrivilegeEscalation: ptr.To(false),
						RunAsNonRoot:             ptr.To(true),
						ReadOnlyRootFilesystem:   ptr.To(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type:             corev1.SeccompProfileTypeRuntimeDefault,
							LocalhostProfile: nil,
						},
					}))

				} else if depl.Name == "argocd-repo-server" {
					Expect(*secContext).To(Equal(corev1.SecurityContext{
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						AllowPrivilegeEscalation: ptr.To(false),
						RunAsNonRoot:             ptr.To(true),
						ReadOnlyRootFilesystem:   ptr.To(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type:             corev1.SeccompProfileTypeRuntimeDefault,
							LocalhostProfile: nil,
						},
					}))

				} else if depl.Name == "argocd-server" {
					Expect(*secContext).To(Equal(corev1.SecurityContext{
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						AllowPrivilegeEscalation: ptr.To(false),
						RunAsNonRoot:             ptr.To(true),
						ReadOnlyRootFilesystem:   ptr.To(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type:             corev1.SeccompProfileTypeRuntimeDefault,
							LocalhostProfile: nil,
						},
					}))

				} else {
					Fail("unrecognized deployment: " + depl.Name)
				}
			}

		})

	})
})
