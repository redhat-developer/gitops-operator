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
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	secretFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/secret"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-113_validate_namespacemanagement", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
			randomNS  *corev1.Namespace
			nsCustom  *corev1.Namespace
			targetNs  *corev1.Namespace

			cleanupFunc1 func()
			cleanupFunc2 func()
			cleanupFunc3 func()
			nmName       string = "nm-test"
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()

			nsCustom, cleanupFunc1 = fixture.CreateNamespaceWithCleanupFunc("test-113-custom")
			randomNS, cleanupFunc2 = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			targetNs, cleanupFunc3 = fixture.CreateNamespaceWithCleanupFunc("test-second-nms")

		})

		AfterEach(func() {

			defer cleanupFunc1()
			defer cleanupFunc2()
			defer cleanupFunc3()

			defer fixture.RestoreSubcriptionToDefault() // revert Subscription at end of test
			fixture.OutputDebugOnFail(randomNS.Name, nsCustom.Name)
		})

		deployArgoCD := func(namespace string, managedNamespaces []argov1beta1api.ManagedNamespaces) *argov1beta1api.ArgoCD {
			argoCDInRandomNS := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: namespace},
				Spec: argov1beta1api.ArgoCDSpec{
					NamespaceManagement: managedNamespaces,
				},
			}
			Expect(k8sClient.Create(ctx, argoCDInRandomNS)).To(Succeed())

			By("waiting for Argo CD to be available")
			Eventually(argoCDInRandomNS, "5m", "5s").Should(argocdFixture.BeAvailable())

			return argoCDInRandomNS
		}

		checkRolesBindings := func(ns string, shouldExist bool) {
			roles := []string{
				"argocd-argocd-server",
				"argocd-argocd-application-controller",
			}
			for _, r := range roles {
				role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: r, Namespace: ns}}
				if shouldExist {
					Eventually(role, "90s", "5s").Should(k8sFixture.ExistByName())
				} else {
					Eventually(role, "3m", "5s").Should(k8sFixture.NotExistByName())
					Consistently(role).Should(k8sFixture.NotExistByName())
				}
			}

			rb := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-server", Namespace: ns}}
			if shouldExist {
				Eventually(rb, "90s", "5s").Should(k8sFixture.ExistByName())
			} else {
				Eventually(rb, "3m", "5s").Should(k8sFixture.NotExistByName())
				Consistently(rb).Should(k8sFixture.NotExistByName())
			}
		}

		verifyNamespaceManagementSecretAndApplication := func(argoCD *argov1beta1api.ArgoCD, nsCustom *corev1.Namespace, enabled bool, expectedNamespaces []string) {
			By("verifying that Argo CD eventually includes this other namespace in its Secret list of managed namespaces")
			defaultClusterConfigSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-default-cluster-config",
					Namespace: argoCD.Namespace,
				},
			}
			Eventually(defaultClusterConfigSecret, "90s", "5s").Should(k8sFixture.ExistByName())

			// check the "namespaces" key in the Secret
			if enabled {
				Eventually(defaultClusterConfigSecret, "90s", "5s").Should(
					secretFixture.HaveStringDataKeyValue(
						"namespaces",
						strings.Join(expectedNamespaces, ","),
					),
				)
				Consistently(defaultClusterConfigSecret).Should(
					secretFixture.HaveStringDataKeyValue(
						"namespaces",
						strings.Join(expectedNamespaces, ","),
					),
				)
			} else {
				Eventually(defaultClusterConfigSecret, "90s", "5s").Should(
					secretFixture.HaveStringDataKeyValue(
						"namespaces",
						argoCD.Namespace,
					),
				)
				Consistently(defaultClusterConfigSecret).Should(
					secretFixture.HaveStringDataKeyValue(
						"namespaces",
						argoCD.Namespace,
					),
				)
			}

			By("creating Argo CD Application targeting the other namespace")
			app := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-113-custom",
					Namespace: argoCD.Namespace,
				},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "test/examples/nginx",
						RepoURL:        "https://github.com/redhat-developer/gitops-operator",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: nsCustom.Name,
						Server:    "https://kubernetes.default.svc",
					},
					Project: "default",
					SyncPolicy: &argocdv1alpha1.SyncPolicy{
						Automated: &argocdv1alpha1.SyncPolicyAutomated{},
					},
				},
			}

			existing := &argocdv1alpha1.Application{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: app.Name, Namespace: app.Namespace}, existing)
			if apierrors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, app)).To(Succeed())
			} else {
				Expect(err).NotTo(HaveOccurred())
			}

			if enabled {
				By("verifying that Argo CD is able to deploy to that other namespace")
				Eventually(app, "4m", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
				Eventually(app, "4m", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))
			} else {
				By("verifying that Argo CD is NOT able to deploy to that other namespace")
				Eventually(app, "4m", "5s").ShouldNot(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))
				Consistently(app, "1m", "5s").ShouldNot(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))
			}
		}

		It("should create Roles/RoleBindings when namespaceManagement is enabled from ArgoCD NamespaceManagement field", func() {
			if fixture.EnvLocalRun() {
				Skip("This test modifies the Subscription/operator deployment env vars, which requires the operator be running on the cluster.")
				return
			}

			By("Create ArgoCD with namespaceManagement field set to true")
			argoCD := deployArgoCD(randomNS.Name, []argov1beta1api.ManagedNamespaces{
				{Name: nsCustom.Name, AllowManagedBy: true},
			})

			By("Enabling namespaceManagement via env var")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ALLOW_NAMESPACE_MANAGEMENT_IN_NAMESPACE_SCOPED_INSTANCES", "true")

			By("Create namespaceManagement CR with the namespace which needs to be managed")
			nm := argov1beta1api.NamespaceManagement{
				ObjectMeta: metav1.ObjectMeta{Name: nmName, Namespace: nsCustom.Name},
				Spec:       argov1beta1api.NamespaceManagementSpec{ManagedBy: randomNS.Name},
			}
			Expect(k8sClient.Create(ctx, &nm)).To(Succeed())

			By("Verify Roles/RoleBindings are created for managed namespace")
			checkRolesBindings(nsCustom.Name, true)

			By("Verify Application and Secret of managed namespace")
			verifyNamespaceManagementSecretAndApplication(argoCD, nsCustom, true, []string{argoCD.Namespace, nsCustom.Name})
		})

		It("should not create roles when namespaceManagement env var is not set", func() {
			if fixture.EnvLocalRun() {
				Skip("This test modifies the Subscription/operator deployment env vars, which requires the operator be running on the cluster.")
				return
			}

			By("Create ArgoCD with namespaceManagement field set to true")
			argoCD := deployArgoCD(randomNS.Name, []argov1beta1api.ManagedNamespaces{
				{Name: nsCustom.Name, AllowManagedBy: true},
			})

			By("Enabling namespaceManagement via env var")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ALLOW_NAMESPACE_MANAGEMENT_IN_NAMESPACE_SCOPED_INSTANCES", "false")

			By("Create namespaceManagement CR with the namespace which needs to be managed")
			nm := argov1beta1api.NamespaceManagement{
				ObjectMeta: metav1.ObjectMeta{Name: nmName, Namespace: nsCustom.Name},
				Spec:       argov1beta1api.NamespaceManagementSpec{ManagedBy: randomNS.Name},
			}
			Expect(k8sClient.Create(ctx, &nm)).To(Succeed())

			By("Verify Roles/RoleBindings are created for managed namespace")
			checkRolesBindings(nsCustom.Name, false)

			By("Verify Application and Secret of managed namespace")
			verifyNamespaceManagementSecretAndApplication(argoCD, nsCustom, false, []string{})
		})

		It("should delete Roles/RoleBindings when namespace management is disabled from ArgoCD NamespaceManagement field", func() {
			if fixture.EnvLocalRun() {
				Skip("This test modifies the Subscription/operator deployment env vars, which requires the operator be running on the cluster.")
				return
			}

			By("Create ArgoCD with namespaceManagement field set to false")
			argoCD := deployArgoCD(randomNS.Name, []argov1beta1api.ManagedNamespaces{
				{Name: nsCustom.Name, AllowManagedBy: false},
			})

			By("Enabling namespace management via env var")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ALLOW_NAMESPACE_MANAGEMENT_IN_NAMESPACE_SCOPED_INSTANCES", "true")

			By("Create namespaceManagement CR")
			nsm := argov1beta1api.NamespaceManagement{
				ObjectMeta: metav1.ObjectMeta{Name: nmName, Namespace: nsCustom.Name},
				Spec:       argov1beta1api.NamespaceManagementSpec{ManagedBy: randomNS.Name},
			}
			Expect(k8sClient.Create(ctx, &nsm)).To(Succeed())

			By("Verify Roles/RoleBindings are deleted from managed namespace")
			checkRolesBindings(nsCustom.Name, false)

			By("Verify Application and Secret of managed namespace")
			verifyNamespaceManagementSecretAndApplication(argoCD, nsCustom, false, []string{argoCD.Namespace, nsCustom.Name})
		})

		It("should support glob pattern(test-*) matching for managed namespaces", func() {
			if fixture.EnvLocalRun() {
				Skip("This test modifies the Subscription/operator deployment env vars, which requires the operator be running on the cluster.")
				return
			}

			By("Create ArgoCD with namespaceManagement field set to true")
			argoCD := deployArgoCD(randomNS.Name, []argov1beta1api.ManagedNamespaces{
				{Name: "test-*", AllowManagedBy: true},
			})

			By("Enabling namespace management via env var")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ALLOW_NAMESPACE_MANAGEMENT_IN_NAMESPACE_SCOPED_INSTANCES", "true")

			By("Create namespaceManagement CR")
			nsm := argov1beta1api.NamespaceManagement{
				ObjectMeta: metav1.ObjectMeta{Name: nmName, Namespace: nsCustom.Name},
				Spec:       argov1beta1api.NamespaceManagementSpec{ManagedBy: randomNS.Name},
			}
			Expect(k8sClient.Create(ctx, &nsm)).To(Succeed())

			By("Verify Roles/RoleBindings are created for managed namespace test-113-custom")
			checkRolesBindings(nsCustom.Name, true) // matches pattern

			By("Create namespaceManagement CR 2")
			nsm1 := argov1beta1api.NamespaceManagement{
				ObjectMeta: metav1.ObjectMeta{Name: "nm-test-1", Namespace: targetNs.Name},
				Spec:       argov1beta1api.NamespaceManagementSpec{ManagedBy: randomNS.Name},
			}
			Expect(k8sClient.Create(ctx, &nsm1)).To(Succeed())

			By("Verify Roles/RoleBindings are created for managed namespace test-second-nms")
			checkRolesBindings(nsm1.Namespace, true) // matches pattern

			By("Verify Application and Secret of managed namespace test-113-custom")
			verifyNamespaceManagementSecretAndApplication(argoCD, nsCustom, true, []string{argoCD.Namespace, nsCustom.Name, targetNs.Name})

			By("Verify Application and Secret of managed namespace test-second-nms")
			verifyNamespaceManagementSecretAndApplication(argoCD, targetNs, true, []string{argoCD.Namespace, nsCustom.Name, targetNs.Name})
		})

		It("should clean up Roles/Rolebindings when NamespaceManagement is removed from ArgoCD", func() {
			if fixture.EnvLocalRun() {
				Skip("This test modifies the Subscription/operator deployment env vars, which requires the operator be running on the cluster.")
				return
			}

			By("Create ArgoCD with namespaceManagement field set to true")
			argoCD := deployArgoCD(randomNS.Name, []argov1beta1api.ManagedNamespaces{
				{Name: nsCustom.Name, AllowManagedBy: true},
			})

			By("Enabling namespace management via env var")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ALLOW_NAMESPACE_MANAGEMENT_IN_NAMESPACE_SCOPED_INSTANCES", "true")

			By("Create namespaceManagement CR")
			nsm := argov1beta1api.NamespaceManagement{
				ObjectMeta: metav1.ObjectMeta{Name: nmName, Namespace: nsCustom.Name},
				Spec:       argov1beta1api.NamespaceManagementSpec{ManagedBy: randomNS.Name},
			}
			Expect(k8sClient.Create(ctx, &nsm)).To(Succeed())

			By("Role/Rolebindings should be created for managed namespace if namespaceManagement CR is present")
			checkRolesBindings(nsCustom.Name, true)

			By("Remove NamespaceManagement to check cleanup of Roles/Rolebindings")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCD), argoCD)).To(Succeed())
			argoCD.Spec.NamespaceManagement = nil
			Expect(k8sClient.Update(ctx, argoCD)).To(Succeed())

			By("Verify Roles/RoleBindings are deleted from managed namespace")
			Eventually(func() bool {
				checkRolesBindings(nsCustom.Name, false)
				return true
			}, "2m", "5s").Should(BeTrue())

			By("Verify Application and Secret of managed namespace when nm is disabled")
			verifyNamespaceManagementSecretAndApplication(argoCD, nsCustom, false, []string{argoCD.Namespace, nsCustom.Name})
		})

		It("should not create roles when ArgoCD CR has no managedBy entry", func() {
			if fixture.EnvLocalRun() {
				Skip("This test modifies the Subscription/operator deployment env vars, which requires the operator be running on the cluster.")
				return
			}

			By("Create ArgoCD with no namespaceManagement field")
			argoCD := deployArgoCD(randomNS.Name, nil)

			By("enabling namespace management via env var")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ALLOW_NAMESPACE_MANAGEMENT_IN_NAMESPACE_SCOPED_INSTANCES", "true")

			By("Verify Roles/RoleBindings are deleted from managed namespace")
			checkRolesBindings(nsCustom.Name, false)

			By("Verify Application and Secret of managed namespace when nm is not present in ArgoCD")
			verifyNamespaceManagementSecretAndApplication(argoCD, nsCustom, false, []string{argoCD.Namespace, nsCustom.Name})
		})

		It("should enable namespace management when env var is true and disable it when set to false", func() {
			if fixture.EnvLocalRun() {
				Skip("This test modifies the Subscription/operator deployment env vars, which requires the operator be running on the cluster.")
				return
			}

			By("Create ArgoCD with namespaceManagement field set to true")
			argoCD := deployArgoCD(randomNS.Name, []argov1beta1api.ManagedNamespaces{
				{Name: nsCustom.Name, AllowManagedBy: true},
			})

			By("Enabling namespace management via env var")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ALLOW_NAMESPACE_MANAGEMENT_IN_NAMESPACE_SCOPED_INSTANCES", "true")

			By("Create namespaceManagement CR with the namespace which needs to be managed")
			nm := argov1beta1api.NamespaceManagement{
				ObjectMeta: metav1.ObjectMeta{Name: nmName, Namespace: nsCustom.Name},
				Spec:       argov1beta1api.NamespaceManagementSpec{ManagedBy: randomNS.Name},
			}
			Expect(k8sClient.Create(ctx, &nm)).To(Succeed())

			By("Verify Roles/RoleBindings are created for managed namespace when env var is true")
			checkRolesBindings(nsCustom.Name, true)

			By("Verify Application and Secret for managed namespace")
			verifyNamespaceManagementSecretAndApplication(argoCD, nsCustom, true, []string{argoCD.Namespace, nsCustom.Name})

			By("Disabling namespace management via env var")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ALLOW_NAMESPACE_MANAGEMENT_IN_NAMESPACE_SCOPED_INSTANCES", "false")

			By("verifying ALLOW_NAMESPACE_MANAGEMENT_IN_NAMESPACE_SCOPED_INSTANCES is 'false'")
			Eventually(func() bool {
				val, err := fixture.GetEnvInOperatorSubscriptionOrDeployment("ALLOW_NAMESPACE_MANAGEMENT_IN_NAMESPACE_SCOPED_INSTANCES")
				if err != nil {
					GinkgoWriter.Println(err)
					return false
				}
				if val == nil {
					return false
				}
				return *val == "false"
			}).Should(BeTrue(), "ALLOW_NAMESPACE_MANAGEMENT_IN_NAMESPACE_SCOPED_INSTANCES should be false")

			By("Verify Roles/RoleBindings are deleted for managed namespace when env var is false")
			checkRolesBindings(nsCustom.Name, false)

			By("Verify Application should not be able to sync to managed namespace when env var is false")
			verifyNamespaceManagementSecretAndApplication(argoCD, nsCustom, false, []string{argoCD.Namespace, nsCustom.Name})
		})
	})
})
