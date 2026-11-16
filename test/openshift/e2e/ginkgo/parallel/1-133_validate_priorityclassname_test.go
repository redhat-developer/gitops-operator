/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/License-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package parallel

import (
	"context"
	"sort"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	argov1beta1api "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-133_validate_priorityclassname", func() {

		var (
			k8sClient                    client.Client
			ctx                          context.Context
			ns                           *corev1.Namespace
			cleanupFunc                  func()
			testPriorityClassName        string
			testPriorityClassNameUpdated string
			createdPriorityClassNames    []string
		)

		// allDeploymentPriorityClasses returns the sorted, deduplicated set of
		// priorityClassName values across all Deployments in ns. When every
		// Deployment carries the same class the result is []string{thatClass}.
		var allDeploymentPriorityClasses = func() []string {
			var list appsv1.DeploymentList
			if err := k8sClient.List(ctx, &list, client.InNamespace(ns.Name)); err != nil {
				return nil
			}
			seen := map[string]struct{}{}
			for _, d := range list.Items {
				seen[d.Spec.Template.Spec.PriorityClassName] = struct{}{}
			}
			result := make([]string, 0, len(seen))
			for name := range seen {
				result = append(result, name)
			}
			sort.Strings(result)
			return result
		}

		// allStatefulSetPriorityClasses returns the sorted, deduplicated set of
		// priorityClassName values across all StatefulSets in ns.
		var allStatefulSetPriorityClasses = func() []string {
			var list appsv1.StatefulSetList
			if err := k8sClient.List(ctx, &list, client.InNamespace(ns.Name)); err != nil {
				return nil
			}
			seen := map[string]struct{}{}
			for _, ss := range list.Items {
				seen[ss.Spec.Template.Spec.PriorityClassName] = struct{}{}
			}
			result := make([]string, 0, len(seen))
			for name := range seen {
				result = append(result, name)
			}
			sort.Strings(result)
			return result
		}

		// ensureTestPriorityClass creates a uniquely named PriorityClass owned by
		// this spec. The Never preemption policy prevents the test's elevated
		// priority workloads from preempting workloads created by parallel tests.
		var ensureTestPriorityClass = func(name string, value int32) {
			preemptionPolicy := corev1.PreemptNever
			pc := &schedulingv1.PriorityClass{
				ObjectMeta:       metav1.ObjectMeta{Name: name},
				Value:            value,
				PreemptionPolicy: &preemptionPolicy,
			}
			Expect(k8sClient.Create(ctx, pc)).To(Succeed())
			createdPriorityClassNames = append(createdPriorityClassNames, name)
		}

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
			ns = nil
			cleanupFunc = nil
			createdPriorityClassNames = nil

			// PriorityClasses are cluster-scoped, so give each spec run unique
			// names to avoid collisions with other parallel workers.
			runID := utilrand.String(8)
			testPriorityClassName = "e2e-test-high-priority-" + runID
			testPriorityClassNameUpdated = "e2e-test-medium-priority-" + runID
		})

		AfterEach(func() {
			if ns != nil {
				fixture.OutputDebugOnFail(ns)
			}
			if cleanupFunc != nil {
				cleanupFunc()
			}

			// Delete only PriorityClasses created by this spec; never sweep fixed
			// cluster-scoped names that another parallel test might own.
			for _, pcName := range createdPriorityClassNames {
				pc := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: pcName}}
				if err := k8sClient.Delete(ctx, pc); err != nil && !apierrors.IsNotFound(err) {
					GinkgoWriter.Printf("Failed to delete PriorityClass %s: %v\n", pcName, err)
				}
			}
			createdPriorityClassNames = nil
		})

		It("verifies spec.priorityClassName is applied to all operator-managed workloads", func() {

			By("creating PriorityClass resources for the test")
			ensureTestPriorityClass(testPriorityClassName, 1000000)
			ensureTestPriorityClass(testPriorityClassNameUpdated, 500000)

			By("creating a namespace-scoped ArgoCD instance with priorityClassName and all optional components enabled")
			ns, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			enabled := true
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					PriorityClassName: testPriorityClassName,
					ApplicationSet: &argov1beta1api.ArgoCDApplicationSet{
						Enabled: &enabled,
					},
					Notifications: argov1beta1api.ArgoCDNotifications{
						Enabled: true,
					},
					SourceHydrator: argov1beta1api.ArgoCDSourceHydratorSpec{
						Enabled: &enabled,
					},
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

			By("verifying all Deployments and StatefulSets have the expected priorityClassName")
			Eventually(allDeploymentPriorityClasses, "2m", "5s").Should(Equal([]string{testPriorityClassName}))
			Eventually(allStatefulSetPriorityClasses, "2m", "5s").Should(Equal([]string{testPriorityClassName}))

			By("updating priorityClassName to a new value")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.PriorityClassName = testPriorityClassNameUpdated
			})

			By("verifying all Deployments and StatefulSets are updated with the new priorityClassName")
			Eventually(allDeploymentPriorityClasses, "3m", "5s").Should(Equal([]string{testPriorityClassNameUpdated}))
			Eventually(allStatefulSetPriorityClasses, "3m", "5s").Should(Equal([]string{testPriorityClassNameUpdated}))

			By("clearing priorityClassName and verifying all workloads no longer have it set")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.PriorityClassName = ""
			})

			Eventually(allDeploymentPriorityClasses, "3m", "5s").Should(Equal([]string{""}))
			Eventually(allStatefulSetPriorityClasses, "3m", "5s").Should(Equal([]string{""}))
		})

		It("verifies that workloads without priorityClassName set use empty string (default behaviour)", func() {
			By("creating a namespace-scoped ArgoCD instance without priorityClassName")
			ns, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			enabled := true
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					ApplicationSet: &argov1beta1api.ArgoCDApplicationSet{
						Enabled: &enabled,
					},
					Notifications: argov1beta1api.ArgoCDNotifications{
						Enabled: true,
					},
					SourceHydrator: argov1beta1api.ArgoCDSourceHydratorSpec{
						Enabled: &enabled,
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying all Deployments and StatefulSets have no priorityClassName set")
			Eventually(allDeploymentPriorityClasses, "2m", "5s").Should(Equal([]string{""}))
			Eventually(allStatefulSetPriorityClasses, "2m", "5s").Should(Equal([]string{""}))
		})
	})
})
