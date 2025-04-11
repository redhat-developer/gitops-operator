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
	"reflect"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	argocdv1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	appFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-023_validate_repo_server_tls", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifying ArgoCD .spec.repo AutoTLS and verifyTLS work as expected", func() {

			By("creating a namespace scoped Argo instance with AutoTLS set to 'openshift'")

			nsTest_1_23_custom, cleanupFn1 := fixture.CreateNamespaceWithCleanupFunc("test-1-23-custom")
			defer cleanupFn1()

			argoCDTest_1_23_custom := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: nsTest_1_23_custom.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Repo: argov1beta1api.ArgoCDRepoSpec{
						AutoTLS: "openshift",
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCDTest_1_23_custom)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and ready, and verifying the expected Secret exists")
			Eventually(argoCDTest_1_23_custom, "3m", "5s").Should(argocdFixture.BeAvailable())

			argocdRepoServerTLSSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "argocd-repo-server-tls", Namespace: nsTest_1_23_custom.Name}}
			Eventually(argocdRepoServerTLSSecret).Should(k8sFixture.ExistByName())

			By("enabling verifyTLS in the ArgoCD instance, and checking that the argocd-server Deployment has expected parameters")
			argocdFixture.Update(argoCDTest_1_23_custom, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Repo.VerifyTLS = true
			})

			Eventually(func() bool {
				depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-server", Namespace: nsTest_1_23_custom.Name}}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(depl), depl); err != nil {
					GinkgoWriter.Println(err)
					return false
				}

				if len(depl.Spec.Template.Spec.Containers) == 0 {
					return false
				}

				cmdList := depl.Spec.Template.Spec.Containers[0].Command

				return reflect.DeepEqual(cmdList, []string{
					"argocd-server",
					"--repo-server-strict-tls",
					"--staticassets",
					"/shared/app",
					"--dex-server",
					"https://argocd-dex-server.test-1-23-custom.svc.cluster.local:5556",
					"--repo-server",
					"argocd-repo-server.test-1-23-custom.svc.cluster.local:8081",
					"--redis",
					"argocd-redis.test-1-23-custom.svc.cluster.local:6379",
					"--loglevel",
					"info",
					"--logformat",
					"text",
				})

			}).Should(BeTrue())

			By("ensuring we can deploy to the namespace via the ArgoCD instance")
			app := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "guestbook", Namespace: nsTest_1_23_custom.Name},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "./test/examples/nginx",
						RepoURL:        "https://github.com/jgwest/gitops-operator",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: nsTest_1_23_custom.Name,
						Server:    "https://kubernetes.default.svc",
					},
					Project: "default",
					SyncPolicy: &argocdv1alpha1.SyncPolicy{
						Automated: &argocdv1alpha1.SyncPolicyAutomated{},
						Retry:     &argocdv1alpha1.RetryStrategy{Limit: int64(5)},
					},
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			Eventually(app, "60s", "1s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app, "60s", "1s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

		})

	})
})
