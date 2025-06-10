package sequential

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-002-validate_backend_service", func() {

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
		})

		It("validates backend service permissions", func() {

			By("checking the openshift-gitops namespace installed by default")
			Eventually(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops"}}).Should(k8sFixture.ExistByName())

			By("checking we have a cluster deployment in the namespace")
			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "openshift-gitops"}}
			Eventually(depl).Should(k8sFixture.ExistByName())
			Eventually(depl, "60s", "3s").Should(deploymentFixture.HaveReplicas(1))
			Eventually(depl, "60s", "3s").Should(deploymentFixture.HaveReadyReplicas(1))

			By("checking Service for cluster exists")
			Eventually(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "openshift-gitops"}}, "60s", "5s").Should(k8sFixture.ExistByName())
		})
	})
})
