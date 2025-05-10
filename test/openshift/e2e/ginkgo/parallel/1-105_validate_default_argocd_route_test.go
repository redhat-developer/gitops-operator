/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package parallel

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

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-105_validate_default_argocd_route", func() {

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

		})

		It("ensure that openshift-gitops Argo CD has correct default settings, and those settings can be changed which will affect the server Route", func() {

			By("verifying Argo CD in openshift-gitops exists and has server route enabled")

			argoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			Eventually(argoCD, "4m", "5s").Should(argocdFixture.BeAvailable())

			Expect(argoCD.Spec.Server.Route.Enabled).To(BeTrue())

			By("verify the argocd-server-tls secret is created by the OpenShift's service CA")

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-server-tls",
					Namespace: argoCD.Namespace,
				},
			}
			Eventually(secret).Should(k8sFixture.ExistByName())
			Expect(secret.Type).To(Equal(corev1.SecretTypeTLS))

			By("verifying gitops server Route exists in openshift-gitops, and has expected default settings")
			serverRoute := &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-server", Namespace: "openshift-gitops"},
			}
			Eventually(serverRoute).Should(k8sFixture.ExistByName())

			Eventually(serverRoute).Should(routeFixture.HavePort(intstr.FromString("https")))
			Eventually(serverRoute).Should(routeFixture.HaveTLS(routev1.TLSTerminationReencrypt, routev1.InsecureEdgeTerminationPolicyRedirect))

			Eventually(serverRoute).Should(routeFixture.HaveTo(routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   "openshift-gitops-server",
				Weight: ptr.To(int32(100)),
			}))

			By("verifying Route ingress has been admitted")
			Eventually(serverRoute).Should(routeFixture.HaveConditionTypeStatus(routev1.RouteAdmitted, corev1.ConditionTrue))

			By("updating Argo CD to use diffferent Termination and InsecureEdgeTerminationPolicy values")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.Server.Route = argov1beta1api.ArgoCDRouteSpec{
					Enabled: true,
					TLS: &routev1.TLSConfig{
						Termination:                   routev1.TLSTerminationPassthrough,
						InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyNone,
					},
				}
			})

			defer func() {
				By("cleaning up openshift-gitops ArgoCD back to default, after test runs")
				argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
					ac.Spec.Server.Route = argov1beta1api.ArgoCDRouteSpec{
						Enabled: true,
						TLS: &routev1.TLSConfig{
							Termination:                   routev1.TLSTerminationReencrypt,
							InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
						},
					}
				})
			}()

			By("verifying ArgoCD CR is reconciled to the new values")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying Argo CD server Route has picked up the new passthrough and insecure termination values")
			Eventually(serverRoute).Should(k8sFixture.ExistByName())

			Eventually(serverRoute).Should(routeFixture.HavePort(intstr.FromString("https")))
			Eventually(serverRoute).Should(routeFixture.HaveTLS(routev1.TLSTerminationPassthrough, routev1.InsecureEdgeTerminationPolicyNone))

			Eventually(serverRoute).Should(routeFixture.HaveTo(routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   "openshift-gitops-server",
				Weight: ptr.To(int32(100)),
			}))

			Eventually(serverRoute).Should(routeFixture.HaveConditionTypeStatus(routev1.RouteAdmitted, corev1.ConditionTrue))

		})

	})
})
