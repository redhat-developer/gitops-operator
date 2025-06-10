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
	argocdv1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	appFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	namespaceFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/namespace"
	osFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/os"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-064_validate_tcp_reset_error_test", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that argocd cli app manifests command will succesfully retrieve app manifests, and tcp reset error will not occur", func() {

			// This test is VERY similar to 1-027.

			openshiftgitopsArgoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			By("verifying openshift-gitops Argo CD instance is available")
			Eventually(openshiftgitopsArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("creating Argo CD Application in openshift-gitops namespace")
			app := &argocdv1alpha1.Application{
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
			defer func() { // cleanup on test exit
				Expect(k8sClient.Delete(ctx, app)).To(Succeed())
			}()

			By("verifying test-1-27-custom NS is created and is managed by openshift-gitops, and Application deploys successfully")
			test_1_27_customNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-1-27-custom"},
			}

			Eventually(test_1_27_customNS, "5m", "5s").Should(k8sFixture.ExistByName())
			Eventually(test_1_27_customNS).Should(namespaceFixture.HaveLabel("argocd.argoproj.io/managed-by", "openshift-gitops"))
			defer func() {
				Expect(k8sClient.Delete(ctx, test_1_27_customNS)).To(Succeed()) // post-test cleanup
			}()

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
						Namespace: test_1_27_customNS.Name,
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

			Eventually(guestbookApp, "4m", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(guestbookApp, "4m", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

			By("verifying we can log in to Argo CD via CLI")
			Expect(argocdFixture.LogInToDefaultArgoCDInstance()).To(Succeed())

			By("retrieving the Argo CD app manifests via CLI, and verifying the command succeeds and that there is no 'TCP reset error' error")

			output, err := osFixture.ExecCommand("argocd", "app", "manifests", "1-27-argocd", "--source", "git", "--revision", "HEAD")
			Expect(err).ToNot(HaveOccurred())
			Expect(output).ToNot(ContainSubstring("Original error: read tcp"), "ERROR: TCP reset error is present in this code")
		})

	})
})
