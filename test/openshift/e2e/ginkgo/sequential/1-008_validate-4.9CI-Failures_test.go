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

package sequential

import (
	"context"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	argocdv1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	applicationFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"

	"github.com/argoproj/gitops-engine/pkg/health"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-008_validate-4.9CI-Failures", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies 4.9 CI failures", func() {

			sourceNS := fixture.CreateNamespace("source-ns")

			By("creating simple namespace-scoped ArgoCD instance with route enabled")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: sourceNS.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Server: argov1beta1api.ArgoCDServerSpec{
						Route: argov1beta1api.ArgoCDRouteSpec{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("creating target-ns namespace which we will deploy into")
			targetNS := fixture.CreateManagedNamespace("target-ns", sourceNS.Name)

			Eventually(argoCD, "4m", "5s").Should(argocdFixture.HavePhase("Available"))

			By("ensuring default AppProject exists in source-ns")
			appProject := &argocdv1alpha1.AppProject{
				ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: sourceNS.Name},
			}
			Eventually(appProject, "1m", "5s").Should(k8sFixture.ExistByName())

			By("ensuring Roles are created in target-ns, which indicates that it is successfully managed")
			role := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-application-controller", Namespace: targetNS.Name},
			}
			Eventually(role, "1m", "5s").Should(k8sFixture.ExistByName())
			role = &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-server", Namespace: targetNS.Name},
			}
			Eventually(role, "1m", "5s").Should(k8sFixture.ExistByName())

			roleBinding := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-application-controller", Namespace: targetNS.Name},
			}
			Eventually(roleBinding, "1m", "5s").Should(k8sFixture.ExistByName())

			roleBinding = &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-server", Namespace: targetNS.Name},
			}
			Eventually(roleBinding, "1m", "5s").Should(k8sFixture.ExistByName())

			By("creating unrestricted role/rolebinding in source-ns NS for source-ns application-controller ServiceAccount")
			sourceNSRoleCreate := rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: "source-ns-openshift-gitops-argocd-application-controller", Namespace: sourceNS.Name},
				Rules: []rbacv1.PolicyRule{{
					APIGroups: []string{"*"},
					Resources: []string{"*"},
					Verbs:     []string{"*"},
				}},
			}
			Expect(k8sClient.Create(ctx, &sourceNSRoleCreate)).To(Succeed())

			sourceNSRoleBindingCreate := rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "source-ns-openshift-gitops-argocd-application-controller", Namespace: sourceNS.Name},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "Role",
					Name:     "source-ns-openshift-gitops-argocd-application-controller",
				},
				Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "argocd-argocd-application-controller"}},
			}
			Expect(k8sClient.Create(ctx, &sourceNSRoleBindingCreate)).To(Succeed())

			By("verifying the role and rolebinding that we created exist") // Not entirely sure why we do this, but I ported it over from the old kuttl test
			sourceNSRole := rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: "source-ns-openshift-gitops-argocd-application-controller", Namespace: sourceNS.Name},
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&sourceNSRole), &sourceNSRole)).To(Succeed())
			Expect(sourceNSRole.Rules).To(Equal(
				[]rbacv1.PolicyRule{{
					APIGroups: []string{"*"},
					Resources: []string{"*"},
					Verbs:     []string{"*"},
				}},
			))

			sourceNSRoleBinding := rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "source-ns-openshift-gitops-argocd-application-controller", Namespace: sourceNS.Name},
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&sourceNSRoleBinding), &sourceNSRoleBinding)).To(Succeed())
			Expect(sourceNSRoleBinding.RoleRef).To(Equal(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     "source-ns-openshift-gitops-argocd-application-controller",
			}))
			Expect(sourceNSRoleBinding.Subjects).To(Equal([]rbacv1.Subject{{
				Kind: "ServiceAccount", Name: "argocd-argocd-application-controller"}}))

			By("creating Argo CD Application which tries to deploy from source-ns instance to target-ns NS")
			app := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "nginx", Namespace: sourceNS.Name},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "test/examples/nginx",
						RepoURL:        "https://github.com/jgwest/gitops-operator",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: targetNS.Name,
						Server:    "https://kubernetes.default.svc",
					},
					Project: "default",
					SyncPolicy: &argocdv1alpha1.SyncPolicy{
						Automated: &argocdv1alpha1.SyncPolicyAutomated{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			By("verifying Argo CD in source-ns is able to deploy to managed namespace target-ns")
			Eventually(app, "60s", "5s").Should(applicationFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app, "60s", "5s").Should(applicationFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

		})

	})
})
