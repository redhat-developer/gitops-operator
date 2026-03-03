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
	"strings"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	argocdv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	appFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/clusterrole"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	persistentvolumeFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/persistentvolume"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-108_alternate_cluster_roles_cluster_scoped_instance", func() {

		var (
			k8sClient    client.Client
			ctx          context.Context
			ns           *corev1.Namespace
			testGitOpsNs *corev1.Namespace
			cleanupFunc  func()
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()

		})

		AfterEach(func() {

			fixture.OutputDebugOnFail(ns)

			if testGitOpsNs != nil {
				Expect(k8sClient.Delete(ctx, testGitOpsNs)).To(Succeed())
			}

			if cleanupFunc != nil {
				cleanupFunc()
			}
		})

		It("verifies that you can add alternate namespaces to ARGOCD_CLUSTER_CONFIG_NAMESPACES, and that the clusterrole and binding created by this feature can be disabled via DefaultClusterScopedRoleDisabled", func() {

			if fixture.EnvLocalRun() {
				Skip("This test does not support local run, as when the controller is running locally there is no env var to modify")
				return
			}

			By("creating new namespace alternate-role")
			ns, cleanupFunc = fixture.CreateNamespaceWithCleanupFunc("alternate-role")

			By("adding alternate-role to ARGOCD_CLUSTER_CONFIG_NAMESPACES in Subscription")

			fixture.SetEnvInOperatorSubscriptionOrDeployment("ARGOCD_CLUSTER_CONFIG_NAMESPACES", "openshift-gitops, alternate-role")

			By("creating an ArgoCD instance in the new namespace, and waiting for it to be available")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			Eventually(argoCD, "8m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying clusterrole is created for the new namespace")
			crAppController := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "argocd-alternate-role-argocd-application-controller",
				},
			}
			Eventually(crAppController).Should(k8sFixture.ExistByName())

			By("verifying the new app controller clusterrole has the expected permissions")
			Expect(crAppController.Rules).To(Equal([]rbacv1.PolicyRule{
				{
					APIGroups: []string{"*"},
					Resources: []string{"*"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					NonResourceURLs: []string{"*"},
					Verbs:           []string{"get", "list"},
				},
				{
					APIGroups: []string{"operators.coreos.com"},
					Resources: []string{"*"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"operator.openshift.io"},
					Resources: []string{"*"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"user.openshift.io"},
					Resources: []string{"*"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"config.openshift.io"},
					Resources: []string{"*"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"console.openshift.io"},
					Resources: []string{"*"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{""},
					Resources: []string{"namespaces", "persistentvolumeclaims", "persistentvolumes", "configmaps"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"rbac.authorization.k8s.io"},
					Resources: []string{"*"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"storage.k8s.io"},
					Resources: []string{"*"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"machine.openshift.io"},
					Resources: []string{"*"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"machineconfiguration.openshift.io"},
					Resources: []string{"*"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{"compliance.openshift.io"},
					Resources: []string{"scansettingbindings"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{""},
					Resources: []string{"serviceaccounts"},
					Verbs:     []string{"impersonate"},
				},
			}))

			By("verifying the new server clusterrole has the expected permissions")
			argocdServerCR := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "argocd-alternate-role-argocd-server",
				},
			}
			Eventually(argocdServerCR).Should(k8sFixture.ExistByName())

			Expect(argocdServerCR.Rules).To(Equal([]rbacv1.PolicyRule{
				{
					APIGroups: []string{"*"},
					Resources: []string{"*"},
					Verbs:     []string{"get", "delete", "patch"},
				},
				{
					APIGroups: []string{"argoproj.io"},
					Resources: []string{"applications", "applicationsets"},
					Verbs:     []string{"list", "watch"},
				},
				{
					APIGroups: []string{""},
					Resources: []string{"events"},
					Verbs:     []string{"list"},
				},
				{
					APIGroups: []string{"batch"},
					Resources: []string{"jobs", "cronjobs", "cronjobs/finalizers"},
					Verbs:     []string{"create", "update"},
				},
			}))

			By("verifying the expected ClusterRoleBindings exist, for the new namespce")
			appControllerCRB := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "argocd-alternate-role-argocd-application-controller",
				},
			}
			Eventually(appControllerCRB).Should(k8sFixture.ExistByName())

			serverCRB := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "argocd-alternate-role-argocd-server",
				},
			}
			Eventually(serverCRB).Should(k8sFixture.ExistByName())

			By("creating a test Argo CD Application that will deploy to the new namespace, from the new namepsace")

			app := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "clusterrole-app",
					Namespace: ns.Name,
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "alternate-role",
						"app.kubernetes.io/name":       "argocd",
						"app.kubernetes.io/part-of":    "argocd",
					},
				},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "customclusterrole",
						RepoURL:        "https://github.com/redhat-developer/openshift-gitops-getting-started.git",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: ns.Name,
						Server:    "https://kubernetes.default.svc",
					},
					Project: "default",
					SyncPolicy: &argocdv1alpha1.SyncPolicy{
						Automated: &argocdv1alpha1.SyncPolicyAutomated{
							Prune:    true,
							SelfHeal: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			By("verifying Argo CD is successfully able to reconcile and deploy the resources of the test Argo CD Application")

			Eventually(app, "4m", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app, "4m", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

			By("verifying that the resources defined in the Application CR are deployed and have the expected values")
			testGitOpsNs = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gitops-ns"},
			}
			Eventually(testGitOpsNs).Should(k8sFixture.ExistByName())

			pv := &corev1.PersistentVolume{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gitops-pv",
				},
			}
			Eventually(pv).Should(k8sFixture.ExistByName())
			Expect(pv.Spec).To(Equal(corev1.PersistentVolumeSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadOnlyMany,
				},
				Capacity: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Mi"),
				},
				PersistentVolumeSource: corev1.PersistentVolumeSource{HostPath: &corev1.HostPathVolumeSource{
					Path: "/mnt/data",
					Type: ptr.To(corev1.HostPathUnset),
				}},
				PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
				StorageClassName:              "manual",
				VolumeMode:                    ptr.To(corev1.PersistentVolumeFilesystem),
			}))

			Eventually(pv, "4m", "5s").Should(persistentvolumeFixture.HavePhase(corev1.VolumeAvailable))

			By("disabling defaultClusterScopedRole for ArgoCD instance")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.DefaultClusterScopedRoleDisabled = true
			})

			By("verifying that cluster-scopes roles/rolebindings are deleted after we disabled defaultClusterScopedRole")
			Eventually(appControllerCRB).Should(k8sFixture.NotExistByName())
			Consistently(appControllerCRB).Should(k8sFixture.NotExistByName())

			Eventually(serverCRB).Should(k8sFixture.NotExistByName())
			Consistently(serverCRB).Should(k8sFixture.NotExistByName())

			Eventually(argocdServerCR).Should(k8sFixture.NotExistByName())
			Consistently(argocdServerCR).Should(k8sFixture.NotExistByName())

			Eventually(crAppController).Should(k8sFixture.NotExistByName())
			Consistently(crAppController).Should(k8sFixture.NotExistByName())

			By("creating a new clusterrole and rolebinding to replace the one we deleted")
			newClusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "argocd-alternate-role-argocd-application-controller",
				},
				Rules: []rbacv1.PolicyRule{
					{
						Verbs:     []string{"get", "list", "watch"},
						APIGroups: []string{"*"},
						Resources: []string{"*"},
					},
					{
						Verbs:     []string{"*"},
						APIGroups: []string{""},
						Resources: []string{"namespaces"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, newClusterRole)).To(Succeed())

			newClusterRoleBinding := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "argocd-alternate-role-argocd-application-controller",
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      "ServiceAccount",
						Name:      "argocd-argocd-application-controller",
						Namespace: "alternate-role",
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "argocd-alternate-role-argocd-application-controller",
				},
			}
			Expect(k8sClient.Create(ctx, newClusterRoleBinding)).To(Succeed())

			By("deleting the resources that were previously created by Argo CD Application deply")
			Expect(k8sClient.Delete(ctx, testGitOpsNs)).To(Succeed())
			Expect(k8sClient.Delete(ctx, pv)).To(Succeed())

			By("verifying that Argo CD says it is not able to deploy the PV due to missing permissions")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(app), app); err != nil {
					GinkgoWriter.Println(err)
					return false
				}

				if app.Status.OperationState == nil {
					GinkgoWriter.Println("app.Status.OperationState is nil")
					return false
				}

				GinkgoWriter.Println(".app.status.operationStatus.message is", app.Status.OperationState.Message)

				return strings.Contains(app.Status.OperationState.Message, "persistentvolumes is forbidden")

			}).Should(BeTrue())

			By("adding permissions back to the clusterrole")
			clusterrole.Update(crAppController, func(cr *rbacv1.ClusterRole) {
				cr.Rules = []rbacv1.PolicyRule{
					{
						Verbs:     []string{"get", "list", "watch"},
						APIGroups: []string{"*"},
						Resources: []string{"*"},
					},
					{
						Verbs:     []string{"*"},
						APIGroups: []string{""},
						Resources: []string{"namespaces", "persistentvolumes"},
					},
				}
			})

			By("verifying that Argo CD is again able to deploy the resources defined in the Argo CD Application")
			testGitOpsNs = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gitops-ns"},
			}
			Eventually(testGitOpsNs, "5m", "5s").Should(k8sFixture.ExistByName())

			pv = &corev1.PersistentVolume{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gitops-pv",
				},
			}
			Eventually(pv, "5m", "5s").Should(k8sFixture.ExistByName())
			Expect(pv.Spec).To(Equal(corev1.PersistentVolumeSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadOnlyMany,
				},
				Capacity: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Mi"),
				},
				PersistentVolumeSource: corev1.PersistentVolumeSource{HostPath: &corev1.HostPathVolumeSource{
					Path: "/mnt/data",
					Type: ptr.To(corev1.HostPathUnset),
				}},
				PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
				StorageClassName:              "manual",
				VolumeMode:                    ptr.To(corev1.PersistentVolumeFilesystem),
			}))
			Eventually(pv, "4m", "5s").Should(persistentvolumeFixture.HavePhase(corev1.VolumeAvailable))
		})

	})
})
