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

	"github.com/argoproj-labs/argocd-operator/api/v1beta1"
	argocdv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	appFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	namespaceFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/namespace"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-027_validate_operand_from_git", func() {

		var (
			ctx              context.Context
			k8sClient        client.Client
			app              *argocdv1alpha1.Application
			test_1_27_custom *corev1.Namespace
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		AfterEach(func() {

			fixture.OutputDebugOnFail(test_1_27_custom, "openshift-gitops")

			if app != nil {
				Expect(k8sClient.Delete(ctx, app)).To(Succeed())
			}
			if test_1_27_custom != nil {
				Expect(k8sClient.Delete(ctx, test_1_27_custom)).To(Succeed())
			}
		})

		It("verifies that a custom Argo CD instance can be deployed by the 'openshift-gitops' Argo CD instance. It also verfies that the custom Argo CD instance is able to deploy a simple application", func() {

			openshiftgitopsArgoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			By("verifying openshift-gitops Argo CD instance is available")
			Eventually(openshiftgitopsArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("creating Argo CD Application in openshift-gitops namespace")
			app = &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "1-27-argocd", Namespace: openshiftgitopsArgoCD.Namespace},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path: "./operator-acceptance/1-027_operand-from-git",
						// TODO: Move this repository to a better location
						RepoURL:        "https://github.com/jannfis/operator-e2e-git",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: openshiftgitopsArgoCD.Namespace,
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

			By("verifying test-1-27-custom NS is created and is managed by openshift-gitops, and Application deploys successfully")
			test_1_27_custom = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-1-27-custom"},
			}

			Eventually(test_1_27_custom, "5m", "5s").Should(k8sFixture.ExistByName())
			Eventually(test_1_27_custom).Should(namespaceFixture.HaveLabel("argocd.argoproj.io/managed-by", "openshift-gitops"))

			Eventually(app, "4m", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app, "4m", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

			By("Verify Argo CD instance deployed by Argo CD becomes available")
			argoCD_test_1_27_custom := &v1beta1.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd",
					Namespace: "test-1-27-custom",
				},
			}
			Eventually(argoCD_test_1_27_custom, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("creating a new simple Argo CD Application which deploys a simple guestbook app. The Application is defined in 'test-1-27-custom namespace'. That namespace is also where the guestbook application resources are deployed.")
			guestbookApp := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "guestbook", Namespace: argoCD_test_1_27_custom.Namespace},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "./test/examples/nginx",
						RepoURL:        "https://github.com/redhat-developer/gitops-operator",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: test_1_27_custom.Name,
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
			Expect(k8sClient.Create(ctx, guestbookApp)).To(Succeed())

			By("verifying expected Argo CD workloads exist in test-1-27-custom namespace")
			deploymentsShouldExist := []string{"argocd-redis", "argocd-server", "argocd-repo-server", "nginx-deployment"}
			for _, depl := range deploymentsShouldExist {
				depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: depl, Namespace: test_1_27_custom.Name}}
				Eventually(depl, "4m", "5s").Should(k8sFixture.ExistByName())
				Eventually(depl, "4m", "5s").Should(deploymentFixture.HaveReplicas(1))
				Eventually(depl, "4m", "5s").Should(deploymentFixture.HaveReadyReplicas(1))
			}

			statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: test_1_27_custom.Name}}
			Eventually(statefulSet).Should(k8sFixture.ExistByName())
			Eventually(statefulSet).Should(statefulsetFixture.HaveReplicas(1))
			Eventually(statefulSet).Should(statefulsetFixture.HaveReadyReplicas(1))

			By("verifying Argo CD instance in test-1-27-custom is available")
			Eventually(argoCD_test_1_27_custom, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying both Argo CD Applications are able to sucessfully deploy")
			Eventually(app, "4m", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app, "4m", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

			Eventually(guestbookApp, "4m", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(guestbookApp, "4m", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

		})

	})
})
