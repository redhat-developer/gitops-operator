package parallel

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gitopsoperatorv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	gitopsserviceFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/gitopsservice"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-120-validate_resource_constraints_gitopsservice_test", func() {
		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()

		})

		It("validates that GitOpsService can take in custom resource constraints", func() {

			By("enabling resource constraints on GitOpsService CR")
			gitopsService := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			}
			Expect(gitopsService).To(k8sFixture.ExistByName())
			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				gs.Spec.Resources = &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"cpu":    resource.MustParse("200m"),
						"memory": resource.MustParse("256Mi"),
					},
					Limits: corev1.ResourceList{
						"cpu":    resource.MustParse("500m"),
						"memory": resource.MustParse("512Mi"),
					},
				}
			})

			// Ensure the change is reverted when the test exits
			defer func() {
				gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
					gs.Spec.Resources = nil
				})
			}()

			By("verifying the openshift-gitops resources have honoured the resource constraints")
			clusterDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "openshift-gitops"}}
			Eventually(clusterDepl).Should(
				And(
					deploymentFixture.HaveResourceRequirements(&corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("200m"),
							"memory": resource.MustParse("256Mi"),
						},
						Limits: corev1.ResourceList{
							"cpu":    resource.MustParse("500m"),
							"memory": resource.MustParse("512Mi"),
						},
					}),
				),
			)
		})
	})
})
