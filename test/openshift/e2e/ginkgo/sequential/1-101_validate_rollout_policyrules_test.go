package sequential

import (
	"context"

	rolloutmanagerv1alpha1 "github.com/argoproj-labs/argo-rollouts-manager/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-101_validate_rollout_policyrules", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {

			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifying Rollouts operator creates the expected policy rules", func() {

			By("creating cluster-scoped Argo Rollouts instance in openshift-gitops RolloutManager")
			rm := &rolloutmanagerv1alpha1.RolloutManager{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-rollout-manager",
					Namespace: "openshift-gitops",
				},
			}
			Expect(k8sClient.Create(ctx, rm)).To(Succeed())

			By("verifying Rollouts ClusterRole contains expected policy rules")
			clusterRole := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "argo-rollouts"}}
			Eventually(clusterRole, "1m", "5s").Should(k8sFixture.ExistByName())
			Expect(clusterRole).To(
				And(
					k8sFixture.HaveLabelWithValue("app.kubernetes.io/component", "argo-rollouts"),
					k8sFixture.HaveLabelWithValue("app.kubernetes.io/name", "argo-rollouts"),
					k8sFixture.HaveLabelWithValue("app.kubernetes.io/part-of", "argo-rollouts"),
				))

			Expect(clusterRole.Rules).To(Equal(
				[]rbacv1.PolicyRule{
					{
						APIGroups: []string{"argoproj.io"},
						Resources: []string{"rollouts", "rollouts/status", "rollouts/finalizers"},
						Verbs:     []string{"get", "list", "watch", "update", "patch"},
					},
					{
						APIGroups: []string{"argoproj.io"},
						Resources: []string{"analysisruns", "analysisruns/finalizers", "experiments", "experiments/finalizers"},
						Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
					},
					{
						APIGroups: []string{"argoproj.io"},
						Resources: []string{"analysistemplates", "clusteranalysistemplates"},
						Verbs:     []string{"get", "list", "watch"},
					},
					{
						APIGroups: []string{"apps"},
						Resources: []string{"replicasets"},
						Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
					},
					{
						APIGroups: []string{"", "apps"},
						Resources: []string{"deployments", "podtemplates"},
						Verbs:     []string{"get", "list", "watch", "update", "patch"},
					},
					{
						APIGroups: []string{""},
						Resources: []string{"services"},
						Verbs:     []string{"get", "list", "watch", "patch", "create", "delete"},
					},
					{
						APIGroups: []string{"coordination.k8s.io"},
						Resources: []string{"leases"},
						Verbs:     []string{"create", "get", "update"},
					},
					{
						APIGroups: []string{""},
						Resources: []string{"secrets", "configmaps"},
						Verbs:     []string{"get", "list", "watch"},
					},
					{
						APIGroups: []string{""},
						Resources: []string{"pods"},
						Verbs:     []string{"list", "update", "watch"},
					},
					{
						APIGroups: []string{""},
						Resources: []string{"pods/eviction"},
						Verbs:     []string{"create"},
					},
					{
						APIGroups: []string{""},
						Resources: []string{"events"},
						Verbs:     []string{"create", "update", "patch"},
					},
					{
						APIGroups: []string{"networking.k8s.io", "extensions"},
						Resources: []string{"ingresses"},
						Verbs:     []string{"create", "get", "list", "watch", "patch"},
					},
					{
						APIGroups: []string{"batch"},
						Resources: []string{"jobs"},
						Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
					},
					{
						APIGroups: []string{"networking.istio.io"},
						Resources: []string{"virtualservices", "destinationrules"},
						Verbs:     []string{"watch", "get", "update", "patch", "list"},
					},
					{
						APIGroups: []string{"split.smi-spec.io"},
						Resources: []string{"trafficsplits"},
						Verbs:     []string{"create", "watch", "get", "update", "patch"},
					},
					{
						APIGroups: []string{"getambassador.io", "x.getambassador.io"},
						Resources: []string{"mappings", "ambassadormappings"},
						Verbs:     []string{"create", "watch", "get", "update", "list", "delete"},
					},
					{
						APIGroups: []string{""},
						Resources: []string{"endpoints"},
						Verbs:     []string{"get"},
					},
					{
						APIGroups: []string{"elbv2.k8s.aws"},
						Resources: []string{"targetgroupbindings"},
						Verbs:     []string{"list", "get"},
					},
					{
						APIGroups: []string{"appmesh.k8s.aws"},
						Resources: []string{"virtualservices"},
						Verbs:     []string{"watch", "get", "list"},
					},
					{
						APIGroups: []string{"appmesh.k8s.aws"},
						Resources: []string{"virtualnodes", "virtualrouters"},
						Verbs:     []string{"watch", "get", "list", "update", "patch"},
					},
					{
						APIGroups: []string{"traefik.containo.us", "traefik.io"},
						Resources: []string{"traefikservices"},
						Verbs:     []string{"watch", "get", "update"},
					},
					{
						APIGroups: []string{"apisix.apache.org"},
						Resources: []string{"apisixroutes"},
						Verbs:     []string{"watch", "get", "update"},
					},
					{
						APIGroups: []string{"route.openshift.io"},
						Resources: []string{"routes"},
						Verbs:     []string{"create", "watch", "get", "update", "patch", "list"},
					},
				}))
		})

	})

})
