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

package parallel

import (
	"context"

	"github.com/argoproj/argo-cd/gitops-engine/pkg/health"
	argocdv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	argov1beta1api "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	appFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deplFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	secretFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/secret"
	ssFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-007_validate_namespace_scoped_install", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
			nsCleanup func()
			ns        *corev1.Namespace
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		AfterEach(func() {

			fixture.OutputDebugOnFail(ns)

			if nsCleanup != nil {
				nsCleanup()
			}

		})

		It("ensures namespace-scoped ArgoCD install has all the expected K8s resources", func() {

			By("creating namespace-scoped ArgoCD")
			ns, nsCleanup = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd",
					Namespace: ns.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{
					Server: argov1beta1api.ArgoCDServerSpec{
						Route: argov1beta1api.ArgoCDRouteSpec{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying expected secrets, deployments, and statefulset")
			secretsShouldExist := []string{"argocd-ca", "argocd-cluster", "argocd-default-cluster-config", "argocd-secret", "argocd-tls"}
			for _, secret := range secretsShouldExist {
				Eventually(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secret, Namespace: ns.Name}}).Should(k8sFixture.ExistByName())
			}

			deploymentsShouldExist := []string{"argocd-redis", "argocd-server", "argocd-repo-server"}
			for _, depl := range deploymentsShouldExist {
				depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: depl, Namespace: ns.Name}}
				Eventually(depl).Should(k8sFixture.ExistByName())
				Eventually(depl).Should(deplFixture.HaveReplicas(1))
				Eventually(depl).Should(deplFixture.HaveReadyReplicas(1))
			}

			statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: ns.Name}}
			Eventually(statefulSet).Should(k8sFixture.ExistByName())
			Eventually(statefulSet).Should(ssFixture.HaveReplicas(1))
			Eventually(statefulSet).Should(ssFixture.HaveReadyReplicas(1))

			By("verifying 'argocd-default-cluster-config' cluster secret")
			clusterSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-default-cluster-config", Namespace: ns.Name},
			}
			Eventually(clusterSecret).Should(secretFixture.HaveStringDataKeyValue("namespaces", ns.Name))

			By("verifying expected ConfigMaps exist")
			configMapsShouldExist := []string{"argocd-cm", "argocd-gpg-keys-cm", "argocd-rbac-cm", "argocd-ssh-known-hosts-cm", "argocd-tls-certs-cm"}
			for _, cmName := range configMapsShouldExist {
				Eventually(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: ns.Name}}).Should(k8sFixture.ExistByName())
			}

			By("verifying expected ServiceAccounts, Roles, and RoleBindings exist")
			rbacNamesShouldExist := []string{"argocd-argocd-application-controller", "argocd-argocd-server"}
			for _, rbacName := range rbacNamesShouldExist {
				Eventually(&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: rbacName, Namespace: ns.Name}}).Should(k8sFixture.ExistByName())
				Eventually(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: rbacName, Namespace: ns.Name}}).Should(k8sFixture.ExistByName())
				Eventually(&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: rbacName, Namespace: ns.Name}}).Should(k8sFixture.ExistByName())
			}

			By("verifying expected ServiceMonitors exist")
			serviceMonitorsShouldExist := []string{"argocd", "argocd-repo-server", "argocd-server"}
			for _, smName := range serviceMonitorsShouldExist {
				Eventually(&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Name: smName, Namespace: ns.Name}}).Should(k8sFixture.ExistByName())
			}

			By("verifying the default AppProject exists")
			Eventually(&argocdv1alpha1.AppProject{ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: ns.Name}}).Should(k8sFixture.ExistByName())

			By("deploying a sample Application into the namespace-scoped Argo CD instance")
			app := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "nginx", Namespace: ns.Name},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "test/examples/nginx",
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
					},
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			By("verifying the Application becomes healthy and synced")
			Eventually(app, "4m", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app, "4m", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

		})

	})
})
