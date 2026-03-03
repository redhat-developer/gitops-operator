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
	"strings"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	configmapFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/configmap"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-033_validate_resource_exclusions", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies setting resource exclusion on the ArgoCD CR will cause it to be set on Argo CD ConfigMap", func() {

			By("creating namespace-scoped Argo CD instance")

			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ResourceExclusions = `- apiGroups:
    - tekton.dev
  clusters:
    - '*'
  kinds:
    -  DaemonSet`
			})

			argocdConfigMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-cm", Namespace: ns.Name}}

			By("verifying ConfigMap has same resource exclusion value as specified in ArgoCD CR")
			Eventually(argocdConfigMap).Should(configmapFixture.HaveStringDataKeyValue("resource.exclusions", `- apiGroups:
    - tekton.dev
  clusters:
    - '*'
  kinds:
    -  DaemonSet`),
			)

		})

		It("verifies default resource exclusions are applied when creating a new ArgoCD instance", func() {

			By("creating namespace-scoped Argo CD instance")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			argocdConfigMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-cm", Namespace: ns.Name}}

			By("verifying default resource exclusions are present in ConfigMap")
			Eventually(argocdConfigMap).Should(configmapFixture.HaveNonEmptyDataKey("resource.exclusions"))

			By("verifying default resource exclusions contain expected API groups and kinds")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "argocd-cm", Namespace: ns.Name}, configMap)
				if err != nil {
					return false
				}

				resourceExclusions := configMap.Data["resource.exclusions"]
				if resourceExclusions == "" {
					return false
				}

				// Verify expected API groups are present
				expectedAPIGroups := []string{
					"discovery.k8s.io",
					"apiregistration.k8s.io",
					"coordination.k8s.io",
					"authentication.k8s.io",
					"authorization.k8s.io",
					"certificates.k8s.io",
					"cert-manager.io",
					"cilium.io",
					"kyverno.io",
					"reports.kyverno.io",
					"wgpolicyk8s.io",
				}

				for _, apiGroup := range expectedAPIGroups {
					if !strings.Contains(resourceExclusions, apiGroup) {
						return false
					}
				}

				// Verify expected resource kinds are present
				expectedKinds := []string{
					"Endpoints",
					"EndpointSlice",
					"APIService",
					"Lease",
					"SelfSubjectReview",
					"TokenReview",
					"LocalSubjectAccessReview",
					"SelfSubjectAccessReview",
					"SelfSubjectRulesReview",
					"SubjectAccessReview",
					"CertificateSigningRequest",
					"CertificateRequest",
					"CiliumIdentity",
					"CiliumEndpoint",
					"CiliumEndpointSlice",
					"PolicyReport",
					"ClusterPolicyReport",
					"EphemeralReport",
					"ClusterEphemeralReport",
					"AdmissionReport",
					"ClusterAdmissionReport",
					"BackgroundScanReport",
					"ClusterBackgroundScanReport",
					"UpdateRequest",
				}

				for _, kind := range expectedKinds {
					if !strings.Contains(resourceExclusions, kind) {
						return false
					}
				}

				return true
			}).Should(BeTrue())

			By("verifying default resource exclusions have exactly 8 entries")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "argocd-cm", Namespace: ns.Name}, configMap)
				if err != nil {
					return false
				}

				resourceExclusions := configMap.Data["resource.exclusions"]
				if resourceExclusions == "" {
					return false
				}

				// Count the number of entries by counting the number of "- apiGroups:" occurrences
				// Each resource exclusion entry starts with "- apiGroups:"
				entryCount := strings.Count(resourceExclusions, "- apiGroups:")

				// Should have exactly 8 entries as actually applied by the system
				return entryCount == 8
			}).Should(BeTrue())
		})

		It("verifies setting custom resource exclusion on the ArgoCD CR will cause it to be set on Argo CD ConfigMap", func() {

			By("creating namespace-scoped Argo CD instance")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("setting custom resource exclusions on the ArgoCD CR")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ResourceExclusions = `- apiGroups:
    - tekton.dev
  clusters:
    - '*'
  kinds:
    - DaemonSet
- apiGroups:
    - kyverno.io
  clusters:
    - '*'
  kinds:
    - Policy
    - ClusterPolicy`
			})

			argocdConfigMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-cm", Namespace: ns.Name}}

			By("verifying ConfigMap has same resource exclusion value as specified in ArgoCD CR")
			Eventually(argocdConfigMap).Should(configmapFixture.HaveStringDataKeyValue("resource.exclusions", `- apiGroups:
    - tekton.dev
  clusters:
    - '*'
  kinds:
    - DaemonSet
- apiGroups:
    - kyverno.io
  clusters:
    - '*'
  kinds:
    - Policy
    - ClusterPolicy`))

			By("verifying custom resource exclusions have exactly 2 entries")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "argocd-cm", Namespace: ns.Name}, configMap)
				if err != nil {
					return false
				}

				resourceExclusions := configMap.Data["resource.exclusions"]
				if resourceExclusions == "" {
					return false
				}

				// Count the number of entries by counting the number of "- apiGroups:" occurrences
				entryCount := strings.Count(resourceExclusions, "- apiGroups:")

				// Should have exactly 2 entries as we set 2 custom exclusions
				return entryCount == 2
			}).Should(BeTrue())
		})

		It("verifies that resource exclusions can be updated and changes are reflected in the ConfigMap", func() {

			By("creating namespace-scoped Argo CD instance")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("setting initial resource exclusions")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ResourceExclusions = `- apiGroups:
    - tekton.dev
  clusters:
    - '*'
  kinds:
    - TaskRun`
			})

			argocdConfigMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-cm", Namespace: ns.Name}}

			By("verifying initial resource exclusions are applied")
			Eventually(argocdConfigMap).Should(configmapFixture.HaveStringDataKeyValue("resource.exclusions", `- apiGroups:
    - tekton.dev
  clusters:
    - '*'
  kinds:
    - TaskRun`))

			By("updating resource exclusions with additional API groups and kinds")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ResourceExclusions = `- apiGroups:
    - tekton.dev
  clusters:
    - '*'
  kinds:
    - TaskRun
    - PipelineRun
- apiGroups:
    - cert-manager.io
  clusters:
    - '*'
  kinds:
    - Certificate
    - CertificateRequest`
			})

			By("verifying updated resource exclusions are applied")
			Eventually(argocdConfigMap).Should(configmapFixture.HaveStringDataKeyValue("resource.exclusions", `- apiGroups:
    - tekton.dev
  clusters:
    - '*'
  kinds:
    - TaskRun
    - PipelineRun
- apiGroups:
    - cert-manager.io
  clusters:
    - '*'
  kinds:
    - Certificate
    - CertificateRequest`))

			By("verifying updated resource exclusions have exactly 2 entries")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "argocd-cm", Namespace: ns.Name}, configMap)
				if err != nil {
					return false
				}

				resourceExclusions := configMap.Data["resource.exclusions"]
				if resourceExclusions == "" {
					return false
				}

				// Count the number of entries by counting the number of "- apiGroups:" occurrences
				entryCount := strings.Count(resourceExclusions, "- apiGroups:")

				// Should have exactly 2 entries as we set 2 updated exclusions
				return entryCount == 2
			}).Should(BeTrue())
		})

		It("verifies multiple resource exclusions with different API groups and kinds work correctly", func() {

			By("creating namespace-scoped Argo CD instance")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("setting comprehensive resource exclusions with multiple API groups and kinds")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ResourceExclusions = `- apiGroups:
    - tekton.dev
  clusters:
    - '*'
  kinds:
    - TaskRun
    - PipelineRun
    - Task
    - Pipeline
- apiGroups:
    - kyverno.io
  clusters:
    - '*'
  kinds:
    - Policy
    - ClusterPolicy
    - PolicyReport
- apiGroups:
    - cert-manager.io
  clusters:
    - '*'
  kinds:
    - Certificate
    - CertificateRequest
    - Issuer
    - ClusterIssuer
- apiGroups:
    - cilium.io
  clusters:
    - '*'
  kinds:
    - CiliumIdentity
    - CiliumEndpoint
    - CiliumEndpointSlice`
			})

			argocdConfigMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-cm", Namespace: ns.Name}}

			By("verifying comprehensive resource exclusions are applied correctly")
			Eventually(argocdConfigMap).Should(configmapFixture.HaveStringDataKeyValue("resource.exclusions", `- apiGroups:
    - tekton.dev
  clusters:
    - '*'
  kinds:
    - TaskRun
    - PipelineRun
    - Task
    - Pipeline
- apiGroups:
    - kyverno.io
  clusters:
    - '*'
  kinds:
    - Policy
    - ClusterPolicy
    - PolicyReport
- apiGroups:
    - cert-manager.io
  clusters:
    - '*'
  kinds:
    - Certificate
    - CertificateRequest
    - Issuer
    - ClusterIssuer
- apiGroups:
    - cilium.io
  clusters:
    - '*'
  kinds:
    - CiliumIdentity
    - CiliumEndpoint
    - CiliumEndpointSlice`))

			By("verifying all expected API groups are present in the ConfigMap")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "argocd-cm", Namespace: ns.Name}, configMap)
				if err != nil {
					return false
				}

				resourceExclusions := configMap.Data["resource.exclusions"]
				if resourceExclusions == "" {
					return false
				}

				// Verify all expected API groups are present
				expectedAPIGroups := []string{"tekton.dev", "kyverno.io", "cert-manager.io", "cilium.io"}
				for _, apiGroup := range expectedAPIGroups {
					if !strings.Contains(resourceExclusions, apiGroup) {
						return false
					}
				}

				// Verify all expected kinds are present
				expectedKinds := []string{
					"TaskRun", "PipelineRun", "Task", "Pipeline",
					"Policy", "ClusterPolicy", "PolicyReport",
					"Certificate", "CertificateRequest", "Issuer", "ClusterIssuer",
					"CiliumIdentity", "CiliumEndpoint", "CiliumEndpointSlice",
				}
				for _, kind := range expectedKinds {
					if !strings.Contains(resourceExclusions, kind) {
						return false
					}
				}

				return true
			}).Should(BeTrue())

			By("verifying comprehensive resource exclusions have exactly 4 entries")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "argocd-cm", Namespace: ns.Name}, configMap)
				if err != nil {
					return false
				}

				resourceExclusions := configMap.Data["resource.exclusions"]
				if resourceExclusions == "" {
					return false
				}

				// Count the number of entries by counting the number of "- apiGroups:" occurrences
				entryCount := strings.Count(resourceExclusions, "- apiGroups:")

				// Should have exactly 4 entries as we set 4 comprehensive exclusions
				return entryCount == 4
			}).Should(BeTrue())
		})

		It("verifies that resource exclusions can be cleared and ConfigMap reverts to default exclusions", func() {

			By("creating namespace-scoped Argo CD instance")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("setting initial resource exclusions")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ResourceExclusions = `- apiGroups:
    - tekton.dev
  clusters:
    - '*'
  kinds:
    - TaskRun`
			})

			argocdConfigMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-cm", Namespace: ns.Name}}

			By("verifying initial resource exclusions are applied")
			Eventually(argocdConfigMap).Should(configmapFixture.HaveStringDataKeyValue("resource.exclusions", `- apiGroups:
    - tekton.dev
  clusters:
    - '*'
  kinds:
    - TaskRun`))

			By("clearing resource exclusions")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ResourceExclusions = ""
			})

			By("verifying resource exclusions revert to default exclusions")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "argocd-cm", Namespace: ns.Name}, configMap)
				if err != nil {
					return false
				}

				resourceExclusions := configMap.Data["resource.exclusions"]
				if resourceExclusions == "" {
					return false
				}

				// Verify that default API groups are present (indicating default exclusions are applied)
				expectedAPIGroups := []string{
					"discovery.k8s.io",
					"apiregistration.k8s.io",
					"coordination.k8s.io",
					"authentication.k8s.io",
					"authorization.k8s.io",
					"certificates.k8s.io",
					"cert-manager.io",
					"cilium.io",
					"kyverno.io",
				}

				for _, apiGroup := range expectedAPIGroups {
					if !strings.Contains(resourceExclusions, apiGroup) {
						return false
					}
				}

				// Verify that the custom tekton.dev exclusion is no longer present
				return !strings.Contains(resourceExclusions, "tekton.dev")
			}).Should(BeTrue())

			By("verifying cleared resource exclusions revert to exactly 8 default entries")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "argocd-cm", Namespace: ns.Name}, configMap)
				if err != nil {
					return false
				}

				resourceExclusions := configMap.Data["resource.exclusions"]
				if resourceExclusions == "" {
					return false
				}

				// Count the number of entries by counting the number of "- apiGroups:" occurrences
				entryCount := strings.Count(resourceExclusions, "- apiGroups:")

				// Should have exactly 8 entries as it reverts to default exclusions
				return entryCount == 8
			}).Should(BeTrue())
		})

	})
})
