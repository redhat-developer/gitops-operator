package sequential

import (
	"context"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-034-validate_custom_roles", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("validates CONTROLLER_CLUSTER_ROLE and SERVER_CLUSTER_ROLE env var supports namespace-scoped instances, and that the default roles are removed", func() {

			if fixture.EnvLocalRun() {
				Skip("Skipping test as LOCAL_RUN env var is set. In this case, it is not possible to set env var on gitops operator controller process.")
				return
			}

			By("creating custom Argo CD Namespace test-1-034-custom")
			test1NS := fixture.CreateNamespace("test-1-034-custom")

			By("creating a test namespace with managed-by label, managed by test1 ns")
			customRoleNS := fixture.CreateManagedNamespace("custom-role-namespace", test1NS.Name)

			By("creating a sample cluster role for application-controller and server")
			clusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "custom-argocd-role",
				},
				Rules: []rbacv1.PolicyRule{
					{
						Verbs:     []string{"list", "watch", "get"},
						APIGroups: []string{"*"},
						Resources: []string{"*"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, clusterRole)).To(Succeed())

			By("creating an Argo CD instance in the new namespace")
			argoCDInTest1NS := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: test1NS.Name},
			}
			Expect(k8sClient.Create(ctx, argoCDInTest1NS)).To(Succeed())

			Eventually(argoCDInTest1NS, "3m", "5s").Should(argocdFixture.BeAvailable())

			By("checking the default managed-by roles are created in the managed namespace")

			Eventually(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-server", Namespace: customRoleNS.Name}}).Should(k8sFixture.ExistByName())

			Eventually(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-application-controller", Namespace: customRoleNS.Name}}).Should(k8sFixture.ExistByName())

			rb := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-server", Namespace: customRoleNS.Name}}
			Eventually(rb).Should(k8sFixture.ExistByName())
			Expect(rb.RoleRef).To(Equal(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     "argocd-argocd-server",
			}))
			Expect(rb.Subjects).To(Equal([]rbacv1.Subject{{
				Kind:      "ServiceAccount",
				Name:      "argocd-argocd-server",
				Namespace: test1NS.Name,
			}}))

			rb = &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-application-controller", Namespace: customRoleNS.Name}}
			Eventually(rb).Should(k8sFixture.ExistByName())
			Expect(rb.RoleRef).To(Equal(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     "argocd-argocd-application-controller",
			}))
			Expect(rb.Subjects).To(Equal([]rbacv1.Subject{{
				Kind:      "ServiceAccount",
				Name:      "argocd-argocd-application-controller",
				Namespace: test1NS.Name,
			}}))

			By("adding custom CONTROLLER and SERVER cluster roles to Subscription or operator Deployment")

			fixture.SetEnvInOperatorSubscriptionOrDeployment("CONTROLLER_CLUSTER_ROLE", "custom-argocd-role")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("SERVER_CLUSTER_ROLE", "custom-argocd-role")

			defer func() {
				By("cleaning up changes to the Subscription or operator Deployment")
				Expect(fixture.RestoreSubcriptionToDefault()).To(Succeed())
			}()

			By("checking if the default roles are removed from the namespace")

			Eventually(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-server", Namespace: customRoleNS.Name}}, "120s", "1s").Should(k8sFixture.NotExistByName())
			Consistently(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-server", Namespace: customRoleNS.Name}}).Should(k8sFixture.NotExistByName())

			Eventually(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-application-controller", Namespace: customRoleNS.Name}}).Should(k8sFixture.NotExistByName())
			Consistently(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-application-controller", Namespace: customRoleNS.Name}}).Should(k8sFixture.NotExistByName())

			Eventually(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-server", Namespace: test1NS.Name}}).Should(k8sFixture.NotExistByName())
			Consistently(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-server", Namespace: test1NS.Name}}).Should(k8sFixture.NotExistByName())

			Eventually(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-application-controller", Namespace: test1NS.Name}}).Should(k8sFixture.NotExistByName())
			Consistently(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-application-controller", Namespace: test1NS.Name}}).Should(k8sFixture.NotExistByName())

			By("checking if the Rolebindings are updated in all the namespaces")

			rb = &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-server", Namespace: customRoleNS.Name}}
			Eventually(rb).Should(k8sFixture.ExistByName())
			Expect(rb.RoleRef).To(Equal(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "custom-argocd-role",
			}))
			Expect(rb.Subjects).To(Equal([]rbacv1.Subject{{
				Kind:      "ServiceAccount",
				Name:      "argocd-argocd-server",
				Namespace: test1NS.Name,
			}}))

			rb = &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-application-controller", Namespace: customRoleNS.Name}}
			Eventually(rb).Should(k8sFixture.ExistByName())
			Expect(rb.RoleRef).To(Equal(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "custom-argocd-role",
			}))
			Expect(rb.Subjects).To(Equal([]rbacv1.Subject{{
				Kind:      "ServiceAccount",
				Name:      "argocd-argocd-application-controller",
				Namespace: test1NS.Name,
			}}))

			rb = &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-server", Namespace: test1NS.Name}}
			Eventually(rb).Should(k8sFixture.ExistByName())
			Expect(rb.RoleRef).To(Equal(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "custom-argocd-role",
			}))
			Expect(rb.Subjects).To(Equal([]rbacv1.Subject{{
				Kind:      "ServiceAccount",
				Name:      "argocd-argocd-server",
				Namespace: test1NS.Name,
			}}))

			rb = &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-application-controller", Namespace: test1NS.Name}}
			Eventually(rb).Should(k8sFixture.ExistByName())
			Expect(rb.RoleRef).To(Equal(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "custom-argocd-role",
			}))
			Expect(rb.Subjects).To(Equal([]rbacv1.Subject{{
				Kind:      "ServiceAccount",
				Name:      "argocd-argocd-application-controller",
				Namespace: test1NS.Name,
			}}))

			By("deleting namespaces created by the test")

			Expect(k8sClient.Delete(ctx, argoCDInTest1NS)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &test1NS)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &customRoleNS)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "custom-argocd-role"}})).To(Succeed())

		})
	})
})
