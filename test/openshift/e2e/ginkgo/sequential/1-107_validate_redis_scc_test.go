package sequential

import (
	"context"
	"strings"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	nodeFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/node"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-107_validate_redis_scc", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {

			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that when Argo CD has HA enabled that the redis pods use restricted-v2 security policy", func() {

			By("verifying we are running on a cluster with at least 3 nodes. This is required for Redis HA")
			nodeFixture.ExpectHasAtLeastXNodes(3)
			// Note: Redis HA requires a cluster which contains multiple nodes

			By("creating simple namespace-scoped Argo CD instance with HA enabled")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					HA: argov1beta1api.ArgoCDHASpec{
						Enabled: true,
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			By("verifying HA Redis becomes available")
			Eventually(argoCD, "6m", "5s").Should(argocdFixture.HaveRedisStatus("Running")) // redis HA takes a while

			By("verifying that argocd-redis-ha pod has scc of restricted-v2")
			var podList corev1.PodList
			Expect(k8sClient.List(ctx, &podList, &client.ListOptions{Namespace: ns.Name})).To(Succeed())

			matchFound := false
			for _, pod := range podList.Items {
				if !strings.Contains(pod.Name, "argocd-redis-ha") {
					continue
				}
				Expect(pod.ObjectMeta.Annotations["openshift.io/scc"]).To(Equal("restricted-v2"))
				matchFound = true
			}
			Expect(matchFound).To(BeTrue())

			By("verifying that argocd-redis-ha-haproxy pod has scc of restricted-v2")
			matchFound = false
			for _, pod := range podList.Items {
				if !strings.Contains(pod.Name, "argocd-redis-ha-haproxy") {
					continue
				}
				Expect(pod.ObjectMeta.Annotations["openshift.io/scc"]).To(Equal("restricted-v2"))
				matchFound = true
			}
			Expect(matchFound).To(BeTrue())

		})

	})

})
