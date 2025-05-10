package sequential

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-026-validate_backend_service_permissions", func() {

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
		})

		It("validates backend service permissions", func() {

			By("verifying that various backend-related resources exist and have the expected values")

			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "openshift-gitops"}}
			Eventually(depl).Should(k8sFixture.ExistByName())
			Expect(depl.Spec.Template.Spec.ServiceAccountName).To(Equal("gitops-service-cluster"))
			Expect(depl.Spec.Template.Spec.DeprecatedServiceAccount).To(Equal("gitops-service-cluster"))

			Eventually(&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "gitops-service-cluster", Namespace: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

			cr := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "gitops-service-cluster"}}
			Eventually(cr).Should(k8sFixture.ExistByName())
			Expect(cr.Rules).To(Equal([]rbacv1.PolicyRule{
				{
					APIGroups: []string{"argoproj.io"},
					Resources: []string{"applications"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{""},
					Resources: []string{"secrets"},
					Verbs:     []string{"get", "list", "watch"},
				},
			}))

			crb := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "gitops-service-cluster"}}
			Eventually(crb).Should(k8sFixture.ExistByName())
			Expect(crb.RoleRef).To(Equal(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "gitops-service-cluster",
			}))
			Expect(crb.Subjects).To(Equal([]rbacv1.Subject{{
				Kind:      "ServiceAccount",
				Name:      "gitops-service-cluster",
				Namespace: "openshift-gitops"},
			}))

		})
	})
})
