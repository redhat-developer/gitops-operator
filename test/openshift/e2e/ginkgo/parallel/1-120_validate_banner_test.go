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
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-120_validate_banner", func() {

		var (
			k8sClient   client.Client
			ctx         context.Context
			ns          *corev1.Namespace
			cleanupFunc func()
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		AfterEach(func() {
			fixture.OutputDebugOnFail(ns)

			if cleanupFunc != nil {
				cleanupFunc()
			}
		})

		It("verifies ArgoCD banner configuration is applied and visible in UI", func() {

			By("creating simple namespace-scoped Argo CD instance")
			ns, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Server: argov1beta1api.ArgoCDServerSpec{
						Route: argov1beta1api.ArgoCDRouteSpec{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("configuring banner settings on the ArgoCD server")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Banner = &argov1beta1api.Banner{
					Content:   "This is a test banner for E2E validation",
					Position:  "top",
					Permanent: true,
					URL:       "https://argo-cd.readthedocs.io/",
				}
			})

			By("verifying the banner configuration is stored in ArgoCD argocd-cm ConfigMap")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "argocd-cm",
					Namespace: ns.Name,
				}, configMap)
				if err != nil {
					GinkgoWriter.Printf("Failed to get argocd-cm ConfigMap: %v\n", err)
					return false
				}

				// Check if banner settings are present in the ConfigMap data
				if configMap.Data == nil {
					GinkgoWriter.Printf("ConfigMap data is nil\n")
					return false
				}

				// Validate specific banner configuration keys
				expectedKeys := map[string]string{
					"ui.bannercontent":   "This is a test banner for E2E validation",
					"ui.bannerposition":  "top",
					"ui.bannerpermanent": "true",
					"ui.bannerurl":       "https://argo-cd.readthedocs.io/",
				}

				for key, expectedValue := range expectedKeys {
					actualValue, exists := configMap.Data[key]
					if !exists {
						GinkgoWriter.Printf("Missing key %s in ConfigMap\n", key)
						return false
					}
					if actualValue != expectedValue {
						GinkgoWriter.Printf("Key %s has value %s, expected %s\n", key, actualValue, expectedValue)
						return false
					}
					GinkgoWriter.Printf("✓ Found correct banner config: %s = %s\n", key, actualValue)
				}

				return true
			}, "120s", "5s").Should(BeTrue())

			By("verifying the ArgoCD server deployment has the expected banner configuration")
			Eventually(func() bool {
				// Get the server service to construct the URL
				service := &corev1.Service{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "argocd-server",
					Namespace: ns.Name,
				}, service)
				if err != nil {
					GinkgoWriter.Printf("Failed to get service: %v\n", err)
					return false
				}

				GinkgoWriter.Printf("Service found: %s\n", service.Name)
				return true
			}, "120s", "5s").Should(BeTrue())
		})

		It("verifies banner settings can be updated dynamically", func() {

			By("creating simple namespace-scoped Argo CD instance")
			ns, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Server: argov1beta1api.ArgoCDServerSpec{
						Route: argov1beta1api.ArgoCDRouteSpec{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("configuring initial banner settings")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Banner = &argov1beta1api.Banner{
					Content:   "Initial banner message",
					Position:  "top",
					Permanent: false,
				}
			})

			By("updating banner settings")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Banner = &argov1beta1api.Banner{
					Content:   "Updated banner message",
					Position:  "bottom",
					Permanent: true,
					URL:       "https://updated.example.com",
				}
			})

			By("verifying the updated configuration is applied in ArgoCD CR")
			Eventually(func() bool {
				updatedArgoCD := &argov1beta1api.ArgoCD{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "argocd",
					Namespace: ns.Name,
				}, updatedArgoCD)
				if err != nil {
					return false
				}

				if updatedArgoCD.Spec.Banner == nil {
					return false
				}

				return updatedArgoCD.Spec.Banner.Content == "Updated banner message" &&
					updatedArgoCD.Spec.Banner.Position == "bottom" &&
					updatedArgoCD.Spec.Banner.Permanent == true &&
					updatedArgoCD.Spec.Banner.URL == "https://updated.example.com"
			}, "120s", "5s").Should(BeTrue())

			By("verifying the updated banner configuration is reflected in ConfigMap")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "argocd-cm",
					Namespace: ns.Name,
				}, configMap)
				if err != nil {
					GinkgoWriter.Printf("Failed to get argocd-cm ConfigMap: %v\n", err)
					return false
				}

				if configMap.Data == nil {
					return false
				}

				// Validate updated banner configuration keys
				expectedUpdatedKeys := map[string]string{
					"ui.bannercontent":   "Updated banner message",
					"ui.bannerposition":  "bottom",
					"ui.bannerpermanent": "true",
					"ui.bannerurl":       "https://updated.example.com",
				}

				for key, expectedValue := range expectedUpdatedKeys {
					actualValue, exists := configMap.Data[key]
					if !exists {
						GinkgoWriter.Printf("Missing updated key %s in ConfigMap\n", key)
						return false
					}
					if actualValue != expectedValue {
						GinkgoWriter.Printf("Updated key %s has value %s, expected %s\n", key, actualValue, expectedValue)
						return false
					}
					GinkgoWriter.Printf("✓ Found correct updated banner config: %s = %s\n", key, actualValue)
				}

				return true
			}, "120s", "5s").Should(BeTrue())
		})

		It("verifies banner can be disabled by removing configuration", func() {

			By("creating simple namespace-scoped Argo CD instance with banner")
			ns, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Server: argov1beta1api.ArgoCDServerSpec{
						Route: argov1beta1api.ArgoCDRouteSpec{
							Enabled: true,
						},
					},
					Banner: &argov1beta1api.Banner{
						Content:   "Banner to be removed",
						Position:  "top",
						Permanent: false,
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("removing banner configuration")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Banner = nil
			})

			By("verifying the banner configuration is removed from ArgoCD CR")
			Eventually(func() bool {
				updatedArgoCD := &argov1beta1api.ArgoCD{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "argocd",
					Namespace: ns.Name,
				}, updatedArgoCD)
				if err != nil {
					return false
				}

				return updatedArgoCD.Spec.Banner == nil
			}, "120s", "5s").Should(BeTrue())

			By("verifying banner fields are removed from ConfigMap")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "argocd-cm",
					Namespace: ns.Name,
				}, configMap)
				if err != nil {
					GinkgoWriter.Printf("Failed to get argocd-cm ConfigMap: %v\n", err)
					return false
				}

				if configMap.Data == nil {
					GinkgoWriter.Printf("ConfigMap data is nil, banner fields should be absent\n")
					return true // No data means no banner fields
				}

				// Check that banner configuration keys are removed
				bannerKeys := []string{
					"ui.bannercontent",
					"ui.bannerposition",
					"ui.bannerpermanent",
					"ui.bannerurl",
				}

				for _, key := range bannerKeys {
					if _, exists := configMap.Data[key]; exists {
						GinkgoWriter.Printf("Banner key %s still exists in ConfigMap after removal\n", key)
						return false
					}
				}

				GinkgoWriter.Printf("✓ All banner configuration keys have been removed from ConfigMap\n")
				return true
			}, "120s", "5s").Should(BeTrue())
		})

		It("verifies all banner properties are correctly mapped to ConfigMap fields", func() {

			By("creating namespace-scoped Argo CD instance with comprehensive banner configuration")
			ns, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Server: argov1beta1api.ArgoCDServerSpec{
						Route: argov1beta1api.ArgoCDRouteSpec{
							Enabled: true,
						},
					},
					Banner: &argov1beta1api.Banner{
						Content:   "Comprehensive Banner Test - All Properties",
						Position:  "bottom",
						Permanent: false,
						URL:       "https://argoproj.github.io/argo-cd/",
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "6m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying all banner properties are correctly stored in argocd-cm ConfigMap")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "argocd-cm",
					Namespace: ns.Name,
				}, configMap)
				if err != nil {
					GinkgoWriter.Printf("Failed to get argocd-cm ConfigMap: %v\n", err)
					return false
				}

				if configMap.Data == nil {
					GinkgoWriter.Printf("ConfigMap data is nil\n")
					return false
				}

				// Validate all banner properties with different values
				expectedMappings := map[string]string{
					"ui.bannercontent":   "Comprehensive Banner Test - All Properties",
					"ui.bannerposition":  "bottom",
					"ui.bannerpermanent": "false", // Note: boolean becomes string "false"
					"ui.bannerurl":       "https://argoproj.github.io/argo-cd/",
				}

				allFieldsCorrect := true
				for configKey, expectedValue := range expectedMappings {
					actualValue, exists := configMap.Data[configKey]
					if exists && actualValue != expectedValue {
						GinkgoWriter.Printf("❌ ConfigMap key %s: got '%s', expected '%s'\n", configKey, actualValue, expectedValue)
						allFieldsCorrect = false
						continue
					}
					GinkgoWriter.Printf("✅ ConfigMap key %s correctly set to: %s\n", configKey, actualValue)
				}

				// Also log all ConfigMap data for debugging
				GinkgoWriter.Printf("Complete ConfigMap data: %+v\n", configMap.Data)

				return allFieldsCorrect
			}, "120s", "5s").Should(BeTrue())

			By("testing banner with only required content field")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Banner = &argov1beta1api.Banner{
					Content: "Minimal Banner - Content Only",
					// Other fields intentionally omitted to test defaults
				}
			})

			By("verifying minimal banner configuration in ConfigMap")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "argocd-cm",
					Namespace: ns.Name,
				}, configMap)
				if err != nil {
					return false
				}

				if configMap.Data == nil {
					return false
				}

				// With minimal config, content should be present, others might have defaults or be absent
				content, hasContent := configMap.Data["ui.bannercontent"]
				if !hasContent || content != "Minimal Banner - Content Only" {
					GinkgoWriter.Printf("Expected minimal banner content not found\n")
					return false
				}

				GinkgoWriter.Printf("✅ Minimal banner configuration validated: content = %s\n", content)
				return true
			}, "120s", "5s").Should(BeTrue())
		})
	})
})
