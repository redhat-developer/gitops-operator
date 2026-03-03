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
	argocdv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-006_validate_machine_config", func() {

		var (
			ctx           context.Context
			k8sClient     client.Client
			defaultArgoCD *argov1beta1api.ArgoCD
			app           *argocdv1alpha1.Application
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		AfterEach(func() {

			fixture.OutputDebugOnFail("openshift-gitops")

			if defaultArgoCD != nil {

				argocdFixture.Update(defaultArgoCD, func(ac *argov1beta1api.ArgoCD) {
					ac.Spec.Repo.Replicas = nil
				})
			}

			if app != nil {
				Expect(k8sClient.Delete(ctx, app)).To(Succeed())
			}
		})

		It("verifies that repo server replicas can be modified via .spec.repo.replicas", func() {

			By("setting the repo server replicas to 2 on openshift-gitops Argo CD")
			var err error
			defaultArgoCD, err = argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())
			Expect(defaultArgoCD).ToNot(BeNil())

			argocdFixture.Update(defaultArgoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Repo.Replicas = ptr.To(int32(2))
			})

			By("creating an Argo CD Application targeting the Argo CD namespace")
			app = &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "validate-machine-config", Namespace: defaultArgoCD.Namespace},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "./test/examples/image",
						RepoURL:        "https://github.com/redhat-developer/gitops-operator",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: defaultArgoCD.Namespace,
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
			Eventually(defaultArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying deployment and statefulset have expected number of replicas, including the repo server which should have 2")
			deploymentsToVerify := []string{
				"openshift-gitops-server",
				"openshift-gitops-redis",
				"openshift-gitops-applicationset-controller",
				"openshift-gitops-repo-server",
			}

			for _, deplToVerify := range deploymentsToVerify {

				depl := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: deplToVerify, Namespace: defaultArgoCD.Namespace},
				}
				Eventually(depl).Should(k8sFixture.ExistByName())

				expectedReadyReplicas := 1
				expectedReplicas := 1

				if deplToVerify == "openshift-gitops-repo-server" {
					expectedReadyReplicas = 2
					expectedReplicas = 2
				}
				Eventually(depl).Should(deployment.HaveReplicas(expectedReplicas))
				Eventually(depl, "2m", "5s").Should(deployment.HaveReadyReplicas(expectedReadyReplicas))
			}

			ss := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-application-controller",
					Namespace: defaultArgoCD.Namespace,
				},
			}
			Eventually(ss).Should(k8sFixture.ExistByName())
			Eventually(ss).Should(statefulsetFixture.HaveReplicas(1))
			Eventually(ss, "2m", "5s").Should(statefulsetFixture.HaveReadyReplicas(1))

			By("verifying the Application has deployed successfully")
			Eventually(app, "4m", "5s").Should(application.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app, "4m", "5s").Should(application.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

			By("updating repo server replicas back to 1")
			argocdFixture.Update(defaultArgoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Repo.Replicas = ptr.To(int32(1))
			})

			By("verifying repo server Deployment moves back to a single replica")
			repoServerDepl := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-repo-server", Namespace: defaultArgoCD.Namespace},
			}
			Eventually(repoServerDepl).Should(k8sFixture.ExistByName())
			Eventually(repoServerDepl).Should(deployment.HaveReplicas(1))
			Eventually(repoServerDepl, "2m", "5s").Should(deployment.HaveReadyReplicas(1))

		})

	})
})
