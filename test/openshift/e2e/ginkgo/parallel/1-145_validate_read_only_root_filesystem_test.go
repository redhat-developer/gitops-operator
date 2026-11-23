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

GITOPS-7256: Add a test validating readOnlyRootFilesystem (GITOPS-6239)
*/

package parallel

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	argov1beta1api "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// isGitOpsOwnedWorkload returns true for Argo CD / GitOps operator-managed workloads.
// This excludes Applications that Argo CD may deploy into the same namespace (e.g. Deployment/cluster).
func isGitOpsOwnedWorkload(labels map[string]string) bool {
	if labels == nil {
		return false
	}
	switch labels["app.kubernetes.io/part-of"] {
	case "argocd", "gitops-plugin", "openshift-gitops-operator":
		return true
	}
	// Operator controller-manager Deployment uses these labels
	if labels["control-plane"] == "controller-manager" {
		return true
	}
	return false
}

func expectReadOnlyRootFilesystemEnabled(container corev1.Container, workloadName string) {
	Expect(container.SecurityContext).ToNot(BeNil(),
		"container %q in %s must have securityContext set", container.Name, workloadName)
	Expect(container.SecurityContext.ReadOnlyRootFilesystem).ToNot(BeNil(),
		"container %q in %s must explicitly set readOnlyRootFilesystem", container.Name, workloadName)
	Expect(*container.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue(),
		"container %q in %s must have readOnlyRootFilesystem set to true", container.Name, workloadName)
}

func verifyPodTemplateReadOnlyRootFilesystem(podTemplate corev1.PodTemplateSpec, workloadName string) {
	for _, container := range podTemplate.Spec.InitContainers {
		expectReadOnlyRootFilesystemEnabled(container, workloadName)
	}
	for _, container := range podTemplate.Spec.Containers {
		expectReadOnlyRootFilesystemEnabled(container, workloadName)
	}
}

func verifyNamespaceWorkloadsReadOnlyRootFilesystem(ctx context.Context, k8sClient client.Client, namespace string) {
	checked := 0

	deplList := &appsv1.DeploymentList{}
	Expect(k8sClient.List(ctx, deplList, client.InNamespace(namespace))).To(Succeed())
	for _, depl := range deplList.Items {
		if !isGitOpsOwnedWorkload(depl.Labels) {
			GinkgoWriter.Println("skipping non-GitOps Deployment:", depl.Name)
			continue
		}
		verifyPodTemplateReadOnlyRootFilesystem(depl.Spec.Template, fmt.Sprintf("Deployment/%s", depl.Name))
		checked++
	}

	ssList := &appsv1.StatefulSetList{}
	Expect(k8sClient.List(ctx, ssList, client.InNamespace(namespace))).To(Succeed())
	for _, ss := range ssList.Items {
		if !isGitOpsOwnedWorkload(ss.Labels) {
			GinkgoWriter.Println("skipping non-GitOps StatefulSet:", ss.Name)
			continue
		}
		verifyPodTemplateReadOnlyRootFilesystem(ss.Spec.Template, fmt.Sprintf("StatefulSet/%s", ss.Name))
		checked++
	}

	Expect(checked).To(BeNumerically(">", 0),
		"expected at least one GitOps-owned workload in namespace %s", namespace)
}

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-145_validate_read_only_root_filesystem", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that Argo CD component workloads explicitly set readOnlyRootFilesystem to true", func() {

			By("creating simple namespace-scoped Argo CD instance")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			DeferCleanup(cleanupFunc)

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
				},
			}

			if fixture.RunningOnOpenShift() {
				argoCD.Spec.SSO = &argov1beta1api.ArgoCDSSOSpec{
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
				}
			}

			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying all Argo CD deployments and statefulsets have readOnlyRootFilesystem set to true")
			verifyNamespaceWorkloadsReadOnlyRootFilesystem(ctx, k8sClient, ns.Name)
		})

		It("verifies that openshift-gitops and operator workloads explicitly set readOnlyRootFilesystem to true", Label("openshift"), func() {

			openshiftGitopsArgoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			By("verifying that the openshift-gitops Argo CD instance exists and is available")
			Eventually(openshiftGitopsArgoCD).Should(k8sFixture.ExistByName())
			Eventually(openshiftGitopsArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying openshift-gitops workloads have readOnlyRootFilesystem set to true")
			verifyNamespaceWorkloadsReadOnlyRootFilesystem(ctx, k8sClient, "openshift-gitops")

			By("verifying openshift-gitops-operator workloads have readOnlyRootFilesystem set to true")
			verifyNamespaceWorkloadsReadOnlyRootFilesystem(ctx, k8sClient, "openshift-gitops-operator")
		})
	})
})
