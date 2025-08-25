package sequential

import (
	"context"

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-113_validate_namespacemanagement", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
			nmName    string = "nm-test"

			randomNS          *corev1.Namespace
			nsTest_1_9_custom *corev1.Namespace
			cleanupFunc1      func()
			cleanupFunc2      func()
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		AfterEach(func() {
			defer cleanupFunc1()
			defer cleanupFunc2()

			fixture.OutputDebugOnFail(randomNS.Name, nsTest_1_9_custom.Name)

		})

		AfterEach(func() {
			fixture.RestoreSubcriptionToDefault() // revert Subscription at end of test
		})

		It("should create Roles/RoleBindings when namespaceManagement is enabled from ArgoCD NamespaceManagement field", func() {
			if fixture.EnvLocalRun() {
				Skip("This test modifies the Subscription/operator deployment env vars, which requires the operator be running on the cluster.")
				return
			}

			By("Enabling namespaceManagement via env var")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ALLOW_NAMESPACE_MANAGEMENT_IN_NAMESPACE_SCOPED_INSTANCES", "true")

			nsTest_1_9_custom, cleanupFunc1 = fixture.CreateNamespaceWithCleanupFunc("test-1-9-custom")

			randomNS, cleanupFunc2 = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			By("creating simple namespace-scoped Argo CD")
			argoCDInRandomNS := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: randomNS.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					NamespaceManagement: []argov1beta1api.ManagedNamespaces{
						{
							Name:           nsTest_1_9_custom.Name,
							AllowManagedBy: true,
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, argoCDInRandomNS)).To(Succeed())

			By("waiting for Argo CD to be available")
			Eventually(argoCDInRandomNS, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("Create namespaceManagement CR with the namespace which needs to be managed")
			nm := argov1beta1api.NamespaceManagement{
				ObjectMeta: metav1.ObjectMeta{Name: nmName, Namespace: nsTest_1_9_custom.Name},
				Spec:       argov1beta1api.NamespaceManagementSpec{ManagedBy: argoCDInRandomNS.Namespace},
			}
			Expect(k8sClient.Create(ctx, &nm)).To(Succeed())

			By("verifying that new roles/rolebindings have be created in the new namespace, that allow the Argo CD instance to manage it")
			argoCDServerRole := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-server", Namespace: nsTest_1_9_custom.Name},
			}
			Eventually(argoCDServerRole).Should(k8sFixture.ExistByName())

			argoCDAppControllerRole := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-application-controller", Namespace: nsTest_1_9_custom.Name},
			}
			Eventually(argoCDAppControllerRole).Should(k8sFixture.ExistByName())

			argoCDServerRB := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-server", Namespace: nsTest_1_9_custom.Name},
			}
			Eventually(argoCDServerRB).Should(k8sFixture.ExistByName())
			Expect(argoCDServerRB.RoleRef).To(Equal(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     "argocd-argocd-server",
			}))

			By("verifying that Argo CD eventually includes this other namespace in its Secret list of managed namespaces")
			defaultClusterConfigSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-default-cluster-config",
					Namespace: argoCDInRandomNS.Namespace,
				},
			}
			Eventually(defaultClusterConfigSecret, "2m", "5s").Should(k8sFixture.ExistByName())

			Eventually(defaultClusterConfigSecret).Should(
				secretFixture.HaveStringDataKeyValue("namespaces", argoCDInRandomNS.Namespace+","+nsTest_1_9_custom.Name))

			By("creating Argo CD Application targeting the other namespace")
			app := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "test-1-9-custom", Namespace: argoCDInRandomNS.Namespace},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "test/examples/nginx",
						RepoURL:        "https://github.com/redhat-developer/gitops-operator",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: nsTest_1_9_custom.Name,
						Server:    "https://kubernetes.default.svc",
					},
					Project: "default",
					SyncPolicy: &argocdv1alpha1.SyncPolicy{
						Automated: &argocdv1alpha1.SyncPolicyAutomated{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			By("verifying that Argo CD is able to deploy to that other namespace")
			Eventually(app, "4m", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app, "4m", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))
		})
	})
})
