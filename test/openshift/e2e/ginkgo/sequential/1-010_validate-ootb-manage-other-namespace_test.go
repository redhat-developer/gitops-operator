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

	argocdv1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	appFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/namespace"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-010_validate-ootb-manage-other-namespace", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that openshift-gitops Argo CD instance is able to manage/unmanage other namespaces via managed-by label", func() {

			By("creating a new namespace that is managed by openshift-gitops Argo CD instance")
			nsTest_1_10_custom, cleanupFunc1 := fixture.CreateManagedNamespaceWithCleanupFunc("test-1-10-custom", "openshift-gitops")
			defer cleanupFunc1()

			openshiftgitopsArgoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			By("verifying openshift-gitops Argo CD instance is available")
			Eventually(openshiftgitopsArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying that new roles/rolebindings have be created in the new namespace, that allow the Argo CD instance to manage it")

			argoCDServerRole := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-argocd-server", Namespace: nsTest_1_10_custom.Name},
			}
			Eventually(argoCDServerRole).Should(k8sFixture.ExistByName())

			argoCDAppControllerRole := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-argocd-application-controller", Namespace: nsTest_1_10_custom.Name},
			}
			Eventually(argoCDAppControllerRole).Should(k8sFixture.ExistByName())

			argoCDServerRB := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-argocd-server", Namespace: nsTest_1_10_custom.Name},
			}
			Eventually(argoCDServerRB).Should(k8sFixture.ExistByName())
			Expect(argoCDServerRB.RoleRef).To(Equal(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     "openshift-gitops-argocd-server",
			}))
			Expect(argoCDServerRB.Subjects).Should(Equal([]rbacv1.Subject{{
				Kind:      "ServiceAccount",
				Name:      "openshift-gitops-argocd-server",
				Namespace: "openshift-gitops",
			}}))

			By("creating a new Argo CD application in openshift-gitops ns, targeting the new namespace")
			app := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "test-1-10-custom", Namespace: openshiftgitopsArgoCD.Namespace},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "test/examples/nginx",
						RepoURL:        "https://github.com/redhat-developer/gitops-operator",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: nsTest_1_10_custom.Name,
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

			By("verifying that Argo CD is able to deploy to that other namespace")
			Eventually(app, "4m", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app, "4m", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

			By("removing managed-by label from the other namespace")
			namespace.Update(nsTest_1_10_custom, func(n *corev1.Namespace) {
				delete(n.ObjectMeta.Labels, "argocd.argoproj.io/managed-by")
			})

			By("verifying Argo CD managed-by roles and rolebindings are removed from other namespace")
			rolesToCheck := []string{"argocd-argocd-server", "argocd-argocd-application-controller"}

			for _, roleToCheck := range rolesToCheck {
				role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: roleToCheck, Namespace: nsTest_1_10_custom.Name}}
				Eventually(role).Should(k8sFixture.NotExistByName())
				Consistently(role).Should(k8sFixture.NotExistByName())
			}

			rbToCheck := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-server", Namespace: nsTest_1_10_custom.Name}}
			Eventually(rbToCheck).Should(k8sFixture.NotExistByName())
			Consistently(rbToCheck).Should(k8sFixture.NotExistByName())
		})

	})
})
