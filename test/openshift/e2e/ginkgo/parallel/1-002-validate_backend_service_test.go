package parallel

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-002-validate_backend_service", func() {

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
		})

		It("validates backend service permissions", Label("openshift"), func() {

			By("checking the openshift-gitops namespace installed by default")
			Eventually(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

			By("checking we have a cluster deployment in the namespace")
			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "openshift-gitops"}}
			Eventually(depl).Should(k8sFixture.ExistByName())
			Eventually(depl, "60s", "3s").Should(deploymentFixture.HaveReplicas(1))
			Eventually(depl, "60s", "3s").Should(deploymentFixture.HaveReadyReplicas(1))
			Expect(depl.Spec.Template.Spec.ServiceAccountName).To(Equal("gitops-service-cluster"))
			Expect(depl.Spec.Template.Spec.DeprecatedServiceAccount).To(Equal("gitops-service-cluster"))

			By("checking Service for cluster exists")
			Eventually(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "openshift-gitops"}}, "60s", "5s").Should(k8sFixture.ExistByName())

			By("checking the cluster ServiceAccount, ClusterRole, and ClusterRoleBinding have the expected values")
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
