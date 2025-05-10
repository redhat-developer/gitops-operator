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

	Context("1-105_validate_label_selector", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {

			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("ensures that ARGOCD_LABEL_SELECTOR controls which ArgoCD CRs are reconciled via operator", func() {

			if fixture.EnvLocalRun() {
				Skip("Skipping as LOCAL_RUN is set. In this case, there is no operator Subscription or Deployment to modify.")
				return
			}

			By("adding ARGOCD_LABEL_SELECTOR foo=bar to Operator")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ARGOCD_LABEL_SELECTOR", "foo=bar")

			defer func() { // Restore subscription to default after test
				Expect(fixture.RestoreSubcriptionToDefault()).To(Succeed())
			}()

			By("creating new namespace-scoped ArgoCD instance in test-argocd")

			ns := fixture.CreateNamespace("test-argocd")

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test1",
					Namespace: ns.Name,
					Labels:    map[string]string{"example": "basic"},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			Consistently(argoCD, "2m", "5s").ShouldNot(argocdFixture.BeAvailable(), "since this ArgoCD does not have foo=bar, it should not be reconciled and thus not become available")

			By("adding foo=bar label to ArgoCD, which should now cause the ArgoCD to be reconciled")

			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				if ac.Labels == nil {
					ac.Labels = map[string]string{}
				}
				ac.Labels["foo"] = "bar"
			})

			By("verifying that ArgoCD becomes available")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			By("adding custom rbac to .spec.rbac of ArgoCD")

			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.RBAC = argov1beta1api.ArgoCDRBACSpec{
					Policy: ptr.To("g, system:cluster-admins, role:admin\ng, cluster-admins, role:admin"),
					Scopes: ptr.To("[email]"),
				}
			})

			By("verifying ArgoCD becomes available after .spec update and that argocd-rbac-cm ConfigMap has expected values from ArgoCD CR rbac field")

			Eventually(argoCD, "1m", "5s").Should(argocdFixture.BeAvailable())

			configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-rbac-cm", Namespace: ns.Name}}
			Eventually(configMap).Should(k8sFixture.ExistByName())
			Expect(strings.TrimSpace(configMap.Data["policy.csv"])).To(Equal("g, system:cluster-admins, role:admin\ng, cluster-admins, role:admin"))
			Expect(configMap.Data["policy.default"]).To(Equal("role:readonly"))
			Expect(configMap.Data["scopes"]).To(Equal("[email]"))

			By("removing foo label from ArgoCD")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				delete(ac.Labels, "foo")
			})

			By("updating RBAC policy field of ArgoCD")

			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.RBAC = argov1beta1api.ArgoCDRBACSpec{
					Policy: ptr.To("g, system:cluster-admins, role:admin\ng, cluster-admins, role:admin"),
					Scopes: ptr.To("[people]"),
				}
			})

			By("verifying that Argo CD argocd-rbac-cm ConfigMap has not changed, since ArgoCD does not have the required foo=bar label")

			configMap = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-rbac-cm", Namespace: ns.Name}}
			Eventually(configMap).Should(k8sFixture.ExistByName())
			Expect(strings.TrimSpace(configMap.Data["policy.csv"])).To(Equal("g, system:cluster-admins, role:admin\ng, cluster-admins, role:admin"))
			Expect(configMap.Data["policy.default"]).To(Equal("role:readonly"))
			Expect(configMap.Data["scopes"]).To(Equal("[email]"))

			Consistently(func() bool {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap); err != nil {
					GinkgoWriter.Println(err)
					return false
				}

				// Fail if scopes is '[people]' even once
				if configMap.Data["scopes"] == "[people]" {
					return false
				}

				return true

			}).Should(BeTrue(), "wait 10 seconds to ensure the ArgoCD is never reconcield")

		})

	})

})
