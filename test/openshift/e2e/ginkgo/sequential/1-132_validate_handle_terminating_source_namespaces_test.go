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
	"fmt"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	argocdv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	appFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	appprojectFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/appproject"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	configmapFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/configmap"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	namespaceFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/namespace"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-132_validate_handle_terminating_source_namespaces", func() {

		var (
			k8sClient                          client.Client
			ctx                                context.Context
			argoCD                             *argov1beta1api.ArgoCD
			appProject                         *argocdv1alpha1.AppProject
			originalArgoCDSourceNamespaces     []string
			originalAppProjectSourceNamespaces []string
			capturedArgoCDSourceNamespaces     bool
			capturedAppProjectSourceNamespaces bool
			terminatingNS                      *corev1.Namespace
			activeNS                           *corev1.Namespace
			destinationNS                      *corev1.Namespace
			configMapTerminatingNS             *corev1.ConfigMap
			terminatingNSCleanupFunc           func()
			activeNSCleanupFunc                func()
			destinationNSCleanupFunc           func()
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		AfterEach(func() {
			fixture.OutputDebugOnFail("openshift-gitops", terminatingNS, activeNS, destinationNS)

			if capturedArgoCDSourceNamespaces {
				argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
					ac.Spec.SourceNamespaces = originalArgoCDSourceNamespaces
				})
			}
			if capturedAppProjectSourceNamespaces {
				appprojectFixture.Update(appProject, func(project *argocdv1alpha1.AppProject) {
					project.Spec.SourceNamespaces = originalAppProjectSourceNamespaces
				})
			}

			if configMapTerminatingNS != nil {
				configmapFixture.Update(configMapTerminatingNS, func(cm *corev1.ConfigMap) {
					cm.Finalizers = nil
				})
			}
			if destinationNSCleanupFunc != nil {
				destinationNSCleanupFunc()
			}
			if activeNSCleanupFunc != nil {
				activeNSCleanupFunc()
			}
			if terminatingNSCleanupFunc != nil {
				terminatingNSCleanupFunc()
			}
		})

		It("ensures that if one source namespace is stuck in terminating, it does not prevent other source namespaces from being managed or deployed to", Label("openshift"), func() {

			By("getting the default cluster-scoped openshift-gitops Argo CD instance")
			var err error
			argoCD, err = argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())
			originalArgoCDSourceNamespaces = append([]string(nil), argoCD.Spec.SourceNamespaces...)
			capturedArgoCDSourceNamespaces = true

			By("creating a source namespace that matches the sourceNamespaces pattern")
			terminatingNS, terminatingNSCleanupFunc = fixture.CreateNamespaceWithCleanupFunc("src-1-132-terminating")

			By("adding sourceNamespaces wildcard to openshift-gitops Argo CD")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.SourceNamespaces = []string{"src-1-132-*"}
			})

			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying terminating source namespace is initially managed")
			Eventually(terminatingNS).Should(namespaceFixture.HaveLabel("argocd.argoproj.io/managed-by-cluster-argocd", "openshift-gitops"))

			terminatingRoleName := fmt.Sprintf("%s_%s", argoCD.Name, terminatingNS.Name)
			Eventually(&rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: terminatingRoleName, Namespace: terminatingNS.Name},
			}).Should(k8sFixture.ExistByName())
			Eventually(&rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: terminatingRoleName, Namespace: terminatingNS.Name},
			}).Should(k8sFixture.ExistByName())

			By("creating a ConfigMap with an unowned finalizer in the terminating source namespace")
			configMapTerminatingNS = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "blocking-finalizer-cm",
					Namespace:  terminatingNS.Name,
					Finalizers: []string{"some.random/finalizer"},
				},
			}
			Expect(k8sClient.Create(ctx, configMapTerminatingNS)).To(Succeed())

			By("deleting the terminating source namespace, which puts it into a simulated stuck Terminating state")
			go func() {
				defer GinkgoRecover()
				Expect(k8sClient.Delete(ctx, terminatingNS)).To(Succeed())
			}()

			By("verifying source namespace moves into terminating state")
			Eventually(terminatingNS).Should(namespaceFixture.HavePhase(corev1.NamespaceTerminating))

			By("creating an active source namespace that matches the same sourceNamespaces pattern")
			activeNS, activeNSCleanupFunc = fixture.CreateNamespaceWithCleanupFunc("src-1-132-active")

			By("verifying Role/RoleBinding are created in the active source namespace")
			activeRoleName := fmt.Sprintf("%s_%s", argoCD.Name, activeNS.Name)
			Eventually(&rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: activeRoleName, Namespace: activeNS.Name},
			}, "2m", "5s").Should(k8sFixture.ExistByName())
			Eventually(&rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: activeRoleName, Namespace: activeNS.Name},
			}, "2m", "5s").Should(k8sFixture.ExistByName())
			Eventually(activeNS).Should(namespaceFixture.HaveLabel("argocd.argoproj.io/managed-by-cluster-argocd", "openshift-gitops"))

			By("verifying ArgoCD CR remains Available")
			Eventually(argoCD, "2m", "5s").Should(argocdFixture.BeAvailable())
			Consistently(argoCD, "20s", "5s").Should(argocdFixture.BeAvailable())

			By("verifying --application-namespaces is set on server and application-controller")
			serverDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-server", Namespace: "openshift-gitops"},
			}
			Eventually(serverDeployment).Should(k8sFixture.ExistByName())
			Eventually(serverDeployment).Should(deploymentFixture.HaveContainerCommandSubstring("--application-namespaces", 0))

			appControllerStatefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-application-controller", Namespace: "openshift-gitops"},
			}
			Eventually(appControllerStatefulSet).Should(k8sFixture.ExistByName())
			Eventually(appControllerStatefulSet).Should(statefulsetFixture.HaveContainerCommandSubstring("--application-namespaces", 0))

			By("creating a managed-by destination namespace for workload deployment")
			// sourceNamespaces only grants RBAC to manage Application CRs in the source NS;
			// OpenShift GitOps requires managed-by Roles to deploy workloads into a destination NS.
			destinationNS, destinationNSCleanupFunc = fixture.CreateManagedNamespaceWithCleanupFunc("dest-1-132", "openshift-gitops")
			Eventually(&rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-argocd-application-controller", Namespace: destinationNS.Name},
			}, "2m", "5s").Should(k8sFixture.ExistByName())

			By("allowing Applications from the active source namespace in the default AppProject")
			appProject = &argocdv1alpha1.AppProject{
				ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "openshift-gitops"},
			}
			Eventually(appProject).Should(k8sFixture.ExistByName())
			originalAppProjectSourceNamespaces = append([]string(nil), appProject.Spec.SourceNamespaces...)
			capturedAppProjectSourceNamespaces = true
			appprojectFixture.Update(appProject, func(project *argocdv1alpha1.AppProject) {
				project.Spec.SourceNamespaces = append(project.Spec.SourceNamespaces, activeNS.Name)
			})

			By("creating a test Argo CD Application in the active source namespace targeting the managed destination")
			app := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "my-app", Namespace: activeNS.Name},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "test/examples/kustomize-guestbook",
						RepoURL:        "https://github.com/redhat-developer/gitops-operator",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: destinationNS.Name,
						Server:    "https://kubernetes.default.svc",
					},
					Project: "default",
					SyncPolicy: &argocdv1alpha1.SyncPolicy{
						Automated: &argocdv1alpha1.SyncPolicyAutomated{
							Prune:    ptr.To(true),
							SelfHeal: ptr.To(true),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			By("verifying Argo CD Application from the active source namespace successfully deploys to the managed destination")
			Eventually(app, "4m", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))
		})
	})
})
