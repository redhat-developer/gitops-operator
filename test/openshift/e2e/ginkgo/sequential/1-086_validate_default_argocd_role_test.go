package sequential

import (
	"context"
	"strings"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-086_validate_default_argocd_role", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {

			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that Argo CD roles are defined as expected in argocd-rbac-cm, based on values in ArgoCD .spec.rbac.defaultPolicy", func() {

			By("verifying default ArgoCD in openshift-gitops is running and has defined expected RBAC values in ConfigMap argocd-rbac-cm")

			argocd, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())
			Eventually(argocd, "4m", "5s").Should(argocdFixture.BeAvailable())
			Expect(argocd.Spec.Server.Route.Enabled).To(BeTrue())

			configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-rbac-cm", Namespace: "openshift-gitops"}}
			Eventually(configMap).Should(k8sFixture.ExistByName())
			Expect(configMap).To(
				And(
					k8sFixture.HaveLabelWithValue("app.kubernetes.io/managed-by", "openshift-gitops"),
					k8sFixture.HaveLabelWithValue("app.kubernetes.io/name", "argocd-rbac-cm"),
					k8sFixture.HaveLabelWithValue("app.kubernetes.io/part-of", "argocd"),
				))

			Expect(strings.TrimSpace(configMap.Data["policy.csv"])).To(Equal("g, system:cluster-admins, role:admin\ng, cluster-admins, role:admin"))
			Expect(configMap.Data["policy.default"]).To(Equal(""))
			Expect(configMap.Data["scopes"]).To(Equal("[groups]"))

			By("creating 3 ArgoCD instances in 3 different namespaces, with different RBAC policies")

			test_1_086_customNS := fixture.CreateNamespace("test-1-086-custom")
			test_1_086_custom2NS := fixture.CreateNamespace("test-1-086-custom2")
			test_1_086_custom3NS := fixture.CreateNamespace("test-1-086-custom3")

			argoCD_default_policy := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-default-policy", Namespace: test_1_086_customNS.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Server: argov1beta1api.ArgoCDServerSpec{
						Route: argov1beta1api.ArgoCDRouteSpec{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD_default_policy)).To(Succeed())

			argoCD_default_policy_empty := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-default-policy-empty", Namespace: test_1_086_custom2NS.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					RBAC: argov1beta1api.ArgoCDRBACSpec{
						DefaultPolicy: ptr.To(""),
					},
					Server: argov1beta1api.ArgoCDServerSpec{
						Route: argov1beta1api.ArgoCDRouteSpec{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD_default_policy_empty)).To(Succeed())

			argoCD_default_policy_admin := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-default-policy-admin", Namespace: test_1_086_custom3NS.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					RBAC: argov1beta1api.ArgoCDRBACSpec{
						DefaultPolicy: ptr.To("role:admin"),
					},
					Server: argov1beta1api.ArgoCDServerSpec{
						Route: argov1beta1api.ArgoCDRouteSpec{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD_default_policy_admin)).To(Succeed())

			By("verifying Argo CD instances become available")

			Eventually(argoCD_default_policy, "2m", "5s").Should(argocdFixture.BeAvailable())
			Eventually(argoCD_default_policy_empty, "2m", "5s").Should(argocdFixture.BeAvailable())
			Eventually(argoCD_default_policy_admin, "2m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying argocd-rbac-cm ConfigMap contains the expected values in each namespace")

			configMap_test_1_086_custom := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-rbac-cm", Namespace: "test-1-086-custom"}}
			Eventually(configMap_test_1_086_custom).Should(k8sFixture.ExistByName())
			Expect(configMap_test_1_086_custom).To(
				And(
					k8sFixture.HaveLabelWithValue("app.kubernetes.io/managed-by", "argocd-default-policy"),
					k8sFixture.HaveLabelWithValue("app.kubernetes.io/name", "argocd-rbac-cm"),
					k8sFixture.HaveLabelWithValue("app.kubernetes.io/part-of", "argocd"),
				))

			Expect(strings.TrimSpace(configMap_test_1_086_custom.Data["policy.csv"])).To(Equal(""))
			Expect(configMap_test_1_086_custom.Data["policy.default"]).To(Equal("role:readonly"))
			Expect(configMap_test_1_086_custom.Data["scopes"]).To(Equal("[groups]"))

			configMap_test_1_086_custom2 := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-rbac-cm", Namespace: "test-1-086-custom2"}}
			Eventually(configMap_test_1_086_custom2).Should(k8sFixture.ExistByName())
			Expect(configMap_test_1_086_custom2).To(
				And(
					k8sFixture.HaveLabelWithValue("app.kubernetes.io/managed-by", "argocd-default-policy-empty"),
					k8sFixture.HaveLabelWithValue("app.kubernetes.io/name", "argocd-rbac-cm"),
					k8sFixture.HaveLabelWithValue("app.kubernetes.io/part-of", "argocd"),
				))

			Expect(strings.TrimSpace(configMap_test_1_086_custom2.Data["policy.csv"])).To(Equal(""))
			Expect(configMap_test_1_086_custom2.Data["policy.default"]).To(Equal(""))
			Expect(configMap_test_1_086_custom2.Data["scopes"]).To(Equal("[groups]"))

			configMap_test_1_086_custom3 := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-rbac-cm", Namespace: "test-1-086-custom3"}}
			Eventually(configMap_test_1_086_custom3).Should(k8sFixture.ExistByName())
			Expect(configMap_test_1_086_custom3).To(
				And(
					k8sFixture.HaveLabelWithValue("app.kubernetes.io/managed-by", "argocd-default-policy-admin"),
					k8sFixture.HaveLabelWithValue("app.kubernetes.io/name", "argocd-rbac-cm"),
					k8sFixture.HaveLabelWithValue("app.kubernetes.io/part-of", "argocd"),
				))

			Expect(strings.TrimSpace(configMap_test_1_086_custom3.Data["policy.csv"])).To(Equal(""))
			Expect(configMap_test_1_086_custom3.Data["policy.default"]).To(Equal("role:admin"))
			Expect(configMap_test_1_086_custom3.Data["scopes"]).To(Equal("[groups]"))

		})

	})

})
