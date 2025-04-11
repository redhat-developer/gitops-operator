package sequential

import (
	"context"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-056_validate_managed-by", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {

			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that managed-by works as expected and that REMOVE_MANAGED_BY_LABEL_ON_ARGOCD_DELETION will remove managed-by label when the related ArgoCD instance is deleted", func() {

			By("creating two namespaces in managed-by relationship")

			test156TargetNS := fixture.CreateNamespace("test-1-56-target")
			test156CustomNS := fixture.CreateManagedNamespace("test-1-56-custom", test156TargetNS.Name)

			By("creating simple Argo CD instance in test-1-56-target NS")
			argoCD_test156Target := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: "test-1-56-target"},
				Spec: argov1beta1api.ArgoCDSpec{
					Server: argov1beta1api.ArgoCDServerSpec{
						Route: argov1beta1api.ArgoCDRouteSpec{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD_test156Target)).To(Succeed())

			By("verifying Argo CD instance is started and expected resources exist")
			Eventually(argoCD_test156Target, "3m", "5s").Should(argocdFixture.BeAvailable())

			redisDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-redis", Namespace: test156TargetNS.Name}}
			Eventually(redisDepl, "60s", "5s").Should(
				And(
					deploymentFixture.HaveReadyReplicas(1),
					deploymentFixture.HaveReplicas(1)))

			repoDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-repo-server", Namespace: test156TargetNS.Name}}
			Eventually(repoDepl, "60s", "5s").Should(
				And(
					deploymentFixture.HaveReadyReplicas(1),
					deploymentFixture.HaveReplicas(1)))

			serverDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-server", Namespace: test156TargetNS.Name}}
			Eventually(serverDepl, "60s", "5s").Should(
				And(
					deploymentFixture.HaveReadyReplicas(1),
					deploymentFixture.HaveReplicas(1)))

			appControllerSS := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-application-controller", Namespace: test156TargetNS.Name}}
			Eventually(appControllerSS, "60s", "5s").Should(
				And(
					statefulsetFixture.HaveReadyReplicas(1),
					statefulsetFixture.HaveReplicas(1)))

			By("deleting Argo CD instance in test-1-56-target")
			Expect(k8sClient.Delete(ctx, argoCD_test156Target)).To(Succeed())

			By("verifying test-1-56-custom is managed by test-1-56-target")

			Eventually(&test156CustomNS).Should(k8sFixture.HaveLabelWithValue("argocd.argoproj.io/managed-by", "test-1-56-target"))

			if !fixture.EnvLocalRun() {

				By("adding REMOVE_MANAGED_BY_LABEL_ON_ARGOCD_DELETION=true to operator Subscription or Deployment")

				fixture.SetEnvInOperatorSubscriptionOrDeployment("REMOVE_MANAGED_BY_LABEL_ON_ARGOCD_DELETION", "true")

				By("creating new 2 new namespaces in managed-by relationship and an Argo CD instance to manage them")

				test156Target2NS := fixture.CreateNamespace("test-1-56-target-2")
				test156Custom2NS := fixture.CreateManagedNamespace("test-1-56-custom-2", test156Target2NS.Name)

				argoCD_test156Target2 := &argov1beta1api.ArgoCD{
					ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-2", Namespace: "test-1-56-target-2"},
					Spec: argov1beta1api.ArgoCDSpec{
						Server: argov1beta1api.ArgoCDServerSpec{
							Route: argov1beta1api.ArgoCDRouteSpec{
								Enabled: true,
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, argoCD_test156Target2)).To(Succeed())

				By("verifying new Argo CD is available along with the expected resources, in the new Namespace")
				Eventually(argoCD_test156Target2, "3m", "5s").Should(argocdFixture.BeAvailable())

				redisDepl2 := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-2-redis", Namespace: test156Target2NS.Name}}
				Eventually(redisDepl2, "60s", "5s").Should(
					And(
						deploymentFixture.HaveReadyReplicas(1),
						deploymentFixture.HaveReplicas(1)))

				repoDepl2 := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-2-repo-server", Namespace: test156Target2NS.Name}}
				Eventually(repoDepl2, "60s", "5s").Should(
					And(
						deploymentFixture.HaveReadyReplicas(1),
						deploymentFixture.HaveReplicas(1)))

				serverDepl2 := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-2-server", Namespace: test156Target2NS.Name}}
				Eventually(serverDepl2, "60s", "5s").Should(
					And(
						deploymentFixture.HaveReadyReplicas(1),
						deploymentFixture.HaveReplicas(1)))

				appControllerSS2 := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-2-application-controller", Namespace: test156Target2NS.Name}}
				Eventually(appControllerSS2, "60s", "5s").Should(
					And(
						statefulsetFixture.HaveReadyReplicas(1),
						statefulsetFixture.HaveReplicas(1)))

				By("deleting the Argo CD in new namespace")
				Expect(k8sClient.Delete(ctx, argoCD_test156Target2)).To(Succeed())

				By("verifying Namespace test-1-56-custom-2 does not have managed-by label for deleted Argo CD namespace")
				Eventually(&test156Custom2NS, "60s", "5s").Should(k8sFixture.NotHaveLabelWithValue("argocd.argoproj.io/managed-by", "test-1-56-target-2"))

				By("removing REMOVE_MANAGED_BY_LABEL_ON_ARGOCD_DELETION from operator Subscription or Deplyoment")
				Expect(fixture.RemoveEnvFromOperatorSubscriptionOrDeployment("REMOVE_MANAGED_BY_LABEL_ON_ARGOCD_DELETION")).To(Succeed())

				By("removing Namespaces created during the test")
				Expect(k8sClient.Delete(ctx, &test156Custom2NS)).To(Succeed())
				Expect(k8sClient.Delete(ctx, &test156Target2NS)).To(Succeed())
			}

			Expect(k8sClient.Delete(ctx, &test156CustomNS)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &test156TargetNS)).To(Succeed())
		})

	})

})
