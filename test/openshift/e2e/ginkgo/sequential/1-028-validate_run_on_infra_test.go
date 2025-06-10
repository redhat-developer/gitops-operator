package sequential

import (
	"context"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gitopsoperatorv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	gitopsserviceFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/gitopsservice"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-028-validate_run_on_infra_test", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("validates that Argo CD runs on infra nodes", func() {

			By("enabling run on infra on GitOpsService CR")
			gitopsService := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			}
			Expect(gitopsService).To(k8sFixture.ExistByName())
			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				gs.Spec.RunOnInfra = true
				gs.Spec.Tolerations = []corev1.Toleration{{Effect: "NoSchedule", Key: "infra", Value: "reserved"}}
			})

			// Ensure the change is reverted when the test exits
			defer func() {
				gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
					gs.Spec.RunOnInfra = false
					gs.Spec.Tolerations = nil
				})
			}()

			By("verifying the openshift-gitops resources have the run on infra labels and tolerations applied")
			clusterDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "openshift-gitops"}}
			Eventually(clusterDepl).Should(
				And(
					deploymentFixture.HaveTemplateSpecNodeSelector(map[string]string{
						"node-role.kubernetes.io/infra": "",
						"kubernetes.io/os":              "linux",
					}),
					deploymentFixture.HaveTolerations([]corev1.Toleration{
						{Key: "infra", Effect: "NoSchedule", Value: "reserved"}}),
				))

			serverDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-server", Namespace: "openshift-gitops"}}
			Eventually(serverDepl).Should(
				And(
					deploymentFixture.HaveTemplateSpecNodeSelector(map[string]string{
						"node-role.kubernetes.io/infra": "",
						"kubernetes.io/os":              "linux",
					}),
					deploymentFixture.HaveTolerations([]corev1.Toleration{
						{Key: "infra", Effect: "NoSchedule", Value: "reserved"}}),
				))

			repoServerDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-repo-server", Namespace: "openshift-gitops"}}
			Eventually(repoServerDepl).Should(
				And(
					deploymentFixture.HaveTemplateSpecNodeSelector(map[string]string{
						"node-role.kubernetes.io/infra": "",
						"kubernetes.io/os":              "linux",
					}),
					deploymentFixture.HaveTolerations([]corev1.Toleration{
						{Key: "infra", Effect: "NoSchedule", Value: "reserved"}}),
				))

			dexServerDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-dex-server", Namespace: "openshift-gitops"}}
			Eventually(dexServerDepl).Should(
				And(
					deploymentFixture.HaveTemplateSpecNodeSelector(map[string]string{
						"node-role.kubernetes.io/infra": "",
						"kubernetes.io/os":              "linux",
					}),
					deploymentFixture.HaveTolerations([]corev1.Toleration{
						{Key: "infra", Effect: "NoSchedule", Value: "reserved"}}),
				))

			redisServerDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-redis", Namespace: "openshift-gitops"}}
			Eventually(redisServerDepl).Should(
				And(
					deploymentFixture.HaveTemplateSpecNodeSelector(map[string]string{
						"node-role.kubernetes.io/infra": "",
						"kubernetes.io/os":              "linux",
					}),
					deploymentFixture.HaveTolerations([]corev1.Toleration{
						{Key: "infra", Effect: "NoSchedule", Value: "reserved"}}),
				))

			appControllerSS := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-application-controller", Namespace: "openshift-gitops"}}
			Eventually(appControllerSS).Should(
				And(
					statefulsetFixture.HaveTemplateSpecNodeSelector(map[string]string{
						"node-role.kubernetes.io/infra": "",
						"kubernetes.io/os":              "linux",
					}),
					statefulsetFixture.HaveTolerations([]corev1.Toleration{
						{Key: "infra", Effect: "NoSchedule", Value: "reserved"}}),
				))

			By("creating a simple namespace-scoped Argo CD instance in another namespace")

			randomNamespace := fixture.CreateRandomE2ETestNamespace()
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: randomNamespace.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			Eventually(argoCD, "90s", "5s").Should(argocdFixture.HavePhase("Available"))

			// verifyNotPresentInNamespace verifies that the various Argo CD resources in the namespace do not have 'run on infra' set
			verifyNotPresentInNamespace := func(ns string) {

				serverDepl = &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-server", Namespace: ns}}
				Eventually(serverDepl).ShouldNot(deploymentFixture.HaveTemplateSpecNodeSelector(map[string]string{
					"node-role.kubernetes.io/infra": "",
					"kubernetes.io/os":              "linux",
				}))
				Eventually(serverDepl).ShouldNot(deploymentFixture.HaveTolerations([]corev1.Toleration{
					{Key: "infra", Effect: "NoSchedule", Value: "reserved"}}))

				repoServer := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-repo-server", Namespace: ns}}
				Eventually(repoServer).ShouldNot(deploymentFixture.HaveTemplateSpecNodeSelector(map[string]string{
					"node-role.kubernetes.io/infra": "",
					"kubernetes.io/os":              "linux",
				}))
				Eventually(repoServer).ShouldNot(deploymentFixture.HaveTolerations([]corev1.Toleration{
					{Key: "infra", Effect: "NoSchedule", Value: "reserved"}}))

				dexServer := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-dex-server", Namespace: ns}}
				Eventually(dexServer).ShouldNot(deploymentFixture.HaveTemplateSpecNodeSelector(map[string]string{
					"node-role.kubernetes.io/infra": "",
					"kubernetes.io/os":              "linux",
				}))
				Eventually(dexServer).ShouldNot(deploymentFixture.HaveTolerations([]corev1.Toleration{
					{Key: "infra", Effect: "NoSchedule", Value: "reserved"}}))

				redisDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-redis", Namespace: ns}}
				Eventually(redisDepl).ShouldNot(deploymentFixture.HaveTemplateSpecNodeSelector(map[string]string{
					"node-role.kubernetes.io/infra": "",
					"kubernetes.io/os":              "linux",
				}))
				Eventually(redisDepl).ShouldNot(deploymentFixture.HaveTolerations([]corev1.Toleration{
					{Key: "infra", Effect: "NoSchedule", Value: "reserved"}}))

				appControllerSS = &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: ns}}
				Eventually(appControllerSS).ShouldNot(statefulsetFixture.HaveTemplateSpecNodeSelector(map[string]string{
					"node-role.kubernetes.io/infra": "",
					"kubernetes.io/os":              "linux",
				}))
				Eventually(appControllerSS).ShouldNot(statefulsetFixture.HaveTolerations([]corev1.Toleration{
					{Key: "infra", Effect: "NoSchedule", Value: "reserved"}}))

			}

			By("verifying that the namespace-scoped Argo CD instance does NOT have run on infra set.")
			verifyNotPresentInNamespace(randomNamespace.Name)
			// Run on infra should only be set on openshift-gitops

			By("disabling run on infra on GitOpsService CR")

			gitopsService = &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			}
			Expect(gitopsService).To(k8sFixture.ExistByName())

			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				gs.Spec.RunOnInfra = false
				gs.Spec.Tolerations = nil
			})

			By("verifying that the resources in openshift-gitops no longer have run on infra tolerations/label set")

			clusterDepl = &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "openshift-gitops"}}
			Eventually(serverDepl).ShouldNot(deploymentFixture.HaveTemplateSpecNodeSelector(map[string]string{
				"node-role.kubernetes.io/infra": "",
				"kubernetes.io/os":              "linux",
			}))

			Eventually(serverDepl).ShouldNot(deploymentFixture.HaveTolerations([]corev1.Toleration{
				{Key: "infra", Effect: "NoSchedule", Value: "reserved"}}),
			)

			verifyNotPresentInNamespace("openshift-gitops")

			// This is required, otherwise StatefulSet will be stuck for every subsequent test. This was ported from kuttl (but we delete the SS rather than updating its replicas)
			appControllerSS = &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-application-controller", Namespace: "openshift-gitops"}}

			Expect(k8sClient.Delete(ctx, appControllerSS)).To(Succeed())
			Eventually(appControllerSS).Should(k8sFixture.ExistByName())

			Eventually(appControllerSS, "2m", "5s").Should(statefulsetFixture.HaveReadyReplicas(1))

		})
	})
})
