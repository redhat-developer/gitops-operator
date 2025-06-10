package sequential

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gitopsoperatorv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
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

	Context("1-071_validate_node_selectors", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {

			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies changes to GitOpsService's nodeselector, tolerations, and runOnInfra will modify Argo CD Deployments and StatefulSets", func() {

			By("ensuring Deployments and StatefulSets have nodeSelector of 'kubernetes.io/os: linux'")

			deploymentNameList := []string{"cluster", "openshift-gitops-server", "openshift-gitops-repo-server", "openshift-gitops-dex-server", "openshift-gitops-redis"}

			for _, deploymentName := range deploymentNameList {
				depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deploymentName, Namespace: "openshift-gitops"}}
				Eventually(depl).Should(k8sFixture.ExistByName())
				Expect(depl.Spec.Template.Spec.NodeSelector).Should(Equal(map[string]string{"kubernetes.io/os": "linux"}))
			}

			appControllerSS := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-application-controller", Namespace: "openshift-gitops"}}
			Eventually(appControllerSS).Should(k8sFixture.ExistByName())
			Expect(appControllerSS.Spec.Template.Spec.NodeSelector).Should(Equal(map[string]string{"kubernetes.io/os": "linux"}))

			By("adding 'nodeSelector: {key1: value1}' to GitOpsService CR")

			gitopsService := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			}
			Expect(gitopsService).To(k8sFixture.ExistByName())

			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				if gs.Spec.NodeSelector == nil {
					gs.Spec.NodeSelector = map[string]string{}
				}

				gs.Spec.NodeSelector["key1"] = "value1"
			})

			By("ensuring Deployments and StatefulSets pick up the change we made to nodeSelector in GitOpsService CR")

			for _, deploymentName := range deploymentNameList {
				depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deploymentName, Namespace: "openshift-gitops"}}
				Eventually(depl).Should(deploymentFixture.HaveTemplateSpecNodeSelector(map[string]string{
					"kubernetes.io/os": "linux",
					"key1":             "value1",
				}))
			}

			Eventually(appControllerSS).Should(statefulsetFixture.HaveTemplateSpecNodeSelector(map[string]string{
				"kubernetes.io/os": "linux",
				"key1":             "value1",
			}))

			By("enabling runOnInfra and setting various tolerations on GitOpsService")

			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				gs.Spec.RunOnInfra = true
				gs.Spec.Tolerations = []corev1.Toleration{{
					Effect: "NoSchedule",
					Key:    "infra",
					Value:  "reserved"}}
			})

			By("ensuring Deployments and StatefulSets pick up the change to nodeSelector and tolerations")

			for _, deploymentName := range deploymentNameList {
				depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deploymentName, Namespace: "openshift-gitops"}}
				Eventually(depl).Should(deploymentFixture.HaveTemplateSpecNodeSelector(map[string]string{
					"kubernetes.io/os":              "linux",
					"key1":                          "value1",
					"node-role.kubernetes.io/infra": "",
				}))
				Eventually(depl).Should(deploymentFixture.HaveTolerations([]corev1.Toleration{{
					Effect: "NoSchedule",
					Key:    "infra",
					Value:  "reserved",
				}}))
			}

			Eventually(appControllerSS).Should(statefulsetFixture.HaveTemplateSpecNodeSelector(map[string]string{
				"kubernetes.io/os":              "linux",
				"key1":                          "value1",
				"node-role.kubernetes.io/infra": "",
			}))
			Eventually(appControllerSS).Should(statefulsetFixture.HaveTolerations([]corev1.Toleration{{
				Effect: "NoSchedule",
				Key:    "infra",
				Value:  "reserved",
			}}))

			By("removing all our previous changes from GitOpsService")

			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				gs.Spec.RunOnInfra = false
				gs.Spec.Tolerations = nil
				gs.Spec.NodeSelector = nil
			})

			By("ensuring Deployments and StatefulSets have the nodeSelector and tolerations removed")

			for _, deploymentName := range deploymentNameList {
				depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deploymentName, Namespace: "openshift-gitops"}}
				Eventually(depl).ShouldNot(deploymentFixture.HaveTemplateSpecNodeSelector(map[string]string{
					"kubernetes.io/os":              "linux",
					"key1":                          "value1",
					"node-role.kubernetes.io/infra": "",
				}))
				Eventually(depl).ShouldNot(deploymentFixture.HaveTolerations([]corev1.Toleration{{
					Effect: "NoSchedule",
					Key:    "infra",
					Value:  "reserved",
				}}))
			}

			Eventually(appControllerSS).ShouldNot(statefulsetFixture.HaveTemplateSpecNodeSelector(map[string]string{
				"kubernetes.io/os":              "linux",
				"key1":                          "value1",
				"node-role.kubernetes.io/infra": "",
			}))
			Eventually(appControllerSS).ShouldNot(statefulsetFixture.HaveTolerations([]corev1.Toleration{{
				Effect: "NoSchedule",
				Key:    "infra",
				Value:  "reserved",
			}}))

			// This is required, otherwise StatefulSet will be stuck for every subsequent test. This was ported from kuttl (but we delete the SS rather than updating its replicas, which is what kuttl did)
			Expect(k8sClient.Delete(ctx, appControllerSS)).To(Succeed())
			Eventually(appControllerSS).Should(k8sFixture.ExistByName())

			Eventually(appControllerSS, "2m", "5s").Should(statefulsetFixture.HaveReadyReplicas(1))

		})

	})

})
