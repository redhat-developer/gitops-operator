package sequential

import (
	"context"

	rolloutmanagerv1alpha1 "github.com/argoproj-labs/argo-rollouts-manager/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-100_validate_rollouts_resources_creation", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {

			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("creates a cluster-scopes Argo Rollouts instance and verifies the expected K8s resources are created", func() {

			By("creating simple cluster-scoped Argo Rollouts instance via RolloutManager in openshift-gitops namespace")

			rm := &rolloutmanagerv1alpha1.RolloutManager{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-rollout-manager",
					Namespace: "openshift-gitops",
				},
			}
			Expect(k8sClient.Create(ctx, rm)).To(Succeed())

			By("verifying all the expected K8s resources exist")
			Eventually(&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "argo-rollouts", Namespace: "openshift-gitops"}}, "120s", "1s").Should(k8sFixture.ExistByName())

			Eventually(&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "argo-rollouts", Namespace: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

			Eventually(&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "argo-rollouts", Namespace: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

			Eventually(&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "argo-rollouts-aggregate-to-admin", Namespace: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

			Eventually(&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "argo-rollouts-aggregate-to-edit", Namespace: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

			Eventually(&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "argo-rollouts-aggregate-to-view", Namespace: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

			Eventually(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "argo-rollouts-notification-secret", Namespace: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argo-rollouts", Namespace: "openshift-gitops"}}
			Eventually(depl).Should(k8sFixture.ExistByName())
			Eventually(depl, "4m", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			Eventually(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "argo-rollouts-metrics", Namespace: "openshift-gitops"}}).Should(k8sFixture.ExistByName())
		})

	})

})
