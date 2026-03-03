package sequential

import (
	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	routeFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/route"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-111_validate_default_argocd_route", func() {

		BeforeEach(func() {

			fixture.EnsureSequentialCleanSlate()
		})

		It("ensuring that default openshift-gitops instance has expected default Argo CD server route, and that it is possible to modify the values on that default instance", func() {

			By("verifying route of openshift-gitops Argo CD instance has expected values")
			openshiftArgoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			Expect(openshiftArgoCD.Spec.Server.Route.Enabled).To(BeTrue())
			Eventually(openshiftArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			serverRoute := &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-server",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(serverRoute).Should(k8sFixture.ExistByName())

			Expect(serverRoute.Spec.Port).To(Equal(&routev1.RoutePort{
				TargetPort: intstr.FromString("https"),
			}))

			Expect(serverRoute.Spec.TLS).To(Equal(&routev1.TLSConfig{
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
				Termination:                   routev1.TLSTerminationReencrypt,
			}))

			Expect(serverRoute.Spec.To).Should(Equal(routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   "openshift-gitops-server",
				Weight: ptr.To(int32(100)),
			}))

			By("verifying Route has admitted ingress")
			Eventually(serverRoute).Should(routeFixture.HaveAdmittedIngress())

			By("verifying the argocd-server-tls secret is created by the OpenShift's service CA")

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-server-tls",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(secret).Should(k8sFixture.ExistByName())
			Expect(secret.Type).Should(Equal(corev1.SecretTypeTLS))

			By("updating ArgoCD CR server route to TLS passthrough and TLS termination policy of none")
			argocdFixture.Update(openshiftArgoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Server.Route.Enabled = true
				ac.Spec.Server.Route.TLS = &routev1.TLSConfig{
					Termination:                   routev1.TLSTerminationPassthrough,
					InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyNone,
				}
			})

			defer argocdFixture.Update(openshiftArgoCD, func(ac *argov1beta1api.ArgoCD) { // Cleanup
				ac.Spec.Server.Route.TLS = nil
			})

			By("verifying server Route has changed to the TLS new values from ArgoCD CR")
			Eventually(openshiftArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			Eventually(serverRoute).Should(k8sFixture.ExistByName())

			Expect(serverRoute.Spec.TLS).To(Equal(&routev1.TLSConfig{
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyNone,
				Termination:                   routev1.TLSTerminationPassthrough,
			}))

		})

	})

})
