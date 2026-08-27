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

package sequential

import (
	"context"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	"github.com/argoproj/argo-cd/gitops-engine/pkg/health"
	argocdv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"

	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-006_validate_machine_config", func() {

		var (
			ctx         context.Context
			k8sClient   client.Client
			ns          *corev1.Namespace
			cleanupFunc func()
			argoCD      *argov1beta1api.ArgoCD
			app         *argocdv1alpha1.Application
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		AfterEach(func() {

			if ns != nil {
				fixture.OutputDebugOnFail(ns.Name)
			}

			// Restore the operator Subscription/Deployment to remove the cluster-config namespace we added
			fixture.RestoreSubcriptionToDefault()

			if cleanupFunc != nil {
				cleanupFunc()
			}
		})

		It("verifies that repo server replicas can be modified via .spec.repo.replicas", Label("fixed"), func() {

			// The Application in this test deploys a cluster-scoped resource (config.openshift.io/v1 Image),
			// so the Argo CD instance must be cluster-scoped. That requires setting an env var on the operator,
			// which is not possible when the operator runs locally.
			if fixture.EnvLocalRun() {
				Skip("Skipping test as LOCAL_RUN env var is set. In this case, it is not possible to set env var on gitops operator controller process.")
				return
			}

			By("creating a new namespace for the Argo CD instance")
			ns, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			By("adding the new namespace to ARGOCD_CLUSTER_CONFIG_NAMESPACES so the instance is cluster-scoped")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ARGOCD_CLUSTER_CONFIG_NAMESPACES", "openshift-gitops, "+ns.Name)

			By("creating a new Argo CD instance within the namespace")
			argoCD = argocdFixture.CreateNewArgoCDInstance("argocd", ns.Name)
			Eventually(argoCD, "8m", "5s").Should(argocdFixture.BeAvailable())

			By("setting the repo server replicas to 2 on the Argo CD instance")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				replicas := int32(2)
				ac.Spec.Repo.Replicas = &replicas
			})

			By("creating an Argo CD Application targeting the Argo CD namespace")
			app = &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "validate-machine-config", Namespace: ns.Name},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "./test/examples/nginx",
						RepoURL:        "https://github.com/redhat-developer/gitops-operator",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: ns.Name,
						Server:    "https://kubernetes.default.svc",
					},
					Project: "default",
					SyncPolicy: &argocdv1alpha1.SyncPolicy{
						Automated: &argocdv1alpha1.SyncPolicyAutomated{},
						Retry: &argocdv1alpha1.RetryStrategy{
							Limit: int64(5),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			By("waiting for Argo CD to become available after the repo server change we made")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying deployment and statefulset have expected number of replicas, including the repo server which should have 2")
			deploymentsToVerify := []string{
				"argocd-server",
				"argocd-redis",
				"argocd-repo-server",
			}

			for _, deplToVerify := range deploymentsToVerify {

				depl := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: deplToVerify, Namespace: ns.Name},
				}
				Eventually(depl).Should(k8sFixture.ExistByName())

				expectedReadyReplicas := 1
				expectedReplicas := 1

				if deplToVerify == "argocd-repo-server" {
					expectedReadyReplicas = 2
					expectedReplicas = 2
				}
				Eventually(depl).Should(deployment.HaveReplicas(expectedReplicas))
				Eventually(depl, "2m", "5s").Should(deployment.HaveReadyReplicas(expectedReadyReplicas))
			}

			ss := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-application-controller",
					Namespace: ns.Name,
				},
			}
			Eventually(ss).Should(k8sFixture.ExistByName())
			Eventually(ss).Should(statefulsetFixture.HaveReplicas(1))
			Eventually(ss, "2m", "5s").Should(statefulsetFixture.HaveReadyReplicas(1))

			By("verifying the Application has deployed successfully")
			Eventually(app, "4m", "5s").Should(application.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app, "4m", "5s").Should(application.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

			By("updating repo server replicas back to 1")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				replicas := int32(1)
				ac.Spec.Repo.Replicas = &replicas
			})

			By("waiting for Argo CD to become available after the repo server change we made")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying repo server Deployment moves back to a single replica")
			repoServerDepl := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-repo-server", Namespace: ns.Name},
			}
			Eventually(repoServerDepl).Should(k8sFixture.ExistByName())
			Eventually(repoServerDepl).Should(deployment.HaveReplicas(1))
			Eventually(repoServerDepl, "2m", "5s").Should(deployment.HaveReadyReplicas(1))

		})

	})
})
