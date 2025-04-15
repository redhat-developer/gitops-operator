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
	appv1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"
	"github.com/argoproj/gitops-engine/pkg/sync/common"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	osappsv1 "github.com/openshift/api/apps/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	applicationFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-054_validate_deploymentconfig", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies a DeploymentConfig can be deployed by Argo CD", func() {

			By("creating simple namespace-scoped Argo CD instance")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

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
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			By("creating an Argo CD Application which will deploy a DeploymentConfig")
			app := &appv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-deploymentconfig",
					Namespace: ns.Name,
				},
				Spec: appv1alpha1.ApplicationSpec{
					Project: "default",
					Source: &appv1alpha1.ApplicationSource{
						RepoURL:        "https://github.com/redhat-developer/gitops-operator",
						Path:           "test/examples/deploymentconfig-example",
						TargetRevision: "HEAD",
					},
					Destination: appv1alpha1.ApplicationDestination{
						Server:    "https://kubernetes.default.svc",
						Namespace: ns.Name,
					},
					SyncPolicy: &appv1alpha1.SyncPolicy{Automated: &appv1alpha1.SyncPolicyAutomated{}},
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			By("verifying Application is healthy and sync operation succeeded")
			Eventually(app).Should(applicationFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app).Should(applicationFixture.HaveOperationStatePhase(common.OperationSucceeded))

			By("verifying DeploymentConfig has 2 replicas")
			dc := &osappsv1.DeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-deploymentconfig", Namespace: ns.Name},
			}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dc), dc); err != nil {
					GinkgoWriter.Println(err)
					return false
				}
				return dc.Status.Replicas == 2
			}, "2m", "1s").Should(BeTrue())

			By("updating Application to instead deploy a DeploymentConfig that has replicas: 0")
			applicationFixture.Update(app, func(a *appv1alpha1.Application) {
				a.Spec.Project = "default"
				a.Spec.Source.Path = "test/examples/deploymentconfig-example_replica_0"
			})

			By("verifying DeploymentConfig now has 0 replicas")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dc), dc); err != nil {
					GinkgoWriter.Println(err)
					return false
				}
				return dc.Status.Replicas == 0
			}, "2m", "1s").Should(BeTrue())

			By("verifying Application is still healthy and operation has succeeded")
			Eventually(app).Should(applicationFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app).Should(applicationFixture.HaveOperationStatePhase(common.OperationSucceeded))

		})

	})
})
