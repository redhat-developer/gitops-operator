/*
Copyright 2025.

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
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	argov1beta1api "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const backendProtocolKey = "nginx.ingress.kubernetes.io/backend-protocol"

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-144_validate_ingress", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		// ingress returns the current state of the named Ingress, or nil if it does not exist.
		getIngress := func(name, namespace string) *networkingv1.Ingress {
			i := &networkingv1.Ingress{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, i); err != nil {
				return nil
			}
			return i
		}

		It("reconciles server and gRPC Ingress creation, updates, and deletion", Label("xks"), func() {

			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			By("creating Argo CD with server and gRPC ingress enabled (server.insecure unset)")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Server: argov1beta1api.ArgoCDServerSpec{
						Ingress: argov1beta1api.ArgoCDIngressSpec{Enabled: true},
						GRPC:    argov1beta1api.ArgoCDServerGRPCSpec{Ingress: argov1beta1api.ArgoCDIngressSpec{Enabled: true}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			serverIngress := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-server", Namespace: ns.Name}}
			grpcIngress := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-grpc", Namespace: ns.Name}}
			Eventually(serverIngress).Should(k8sFixture.ExistByName())
			Eventually(grpcIngress).Should(k8sFixture.ExistByName())

			By("verifying the secure default points the server ingress at the https backend")
			Eventually(func(g Gomega) {
				i := getIngress(serverIngress.Name, ns.Name)
				g.Expect(i).ToNot(BeNil())
				g.Expect(i.Annotations).To(HaveKeyWithValue(backendProtocolKey, "HTTPS"))
				g.Expect(i.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name).To(Equal("https"))
			}, "1m", "2s").Should(Succeed())
			Expect(getIngress(grpcIngress.Name, ns.Name).Annotations).To(HaveKeyWithValue(backendProtocolKey, "GRPC"))

			By("adding an annotation as another controller would")
			k8sFixture.Update(serverIngress, func(o client.Object) {
				o.(*networkingv1.Ingress).Annotations["cert-manager.io/issuer"] = "external"
			})

			By("updating the CR: insecure, custom annotation, class name, path")
			argocdFixture.Update(argoCD, func(a *argov1beta1api.ArgoCD) {
				a.Spec.Server.Insecure = true
				a.Spec.Server.Ingress.Annotations = map[string]string{"example.com/custom": "yes"}
				a.Spec.Server.Ingress.IngressClassName = ptr.To("nginx")
				a.Spec.Server.Ingress.Path = "/argo"
			})

			Eventually(func(g Gomega) {
				i := getIngress(serverIngress.Name, ns.Name)
				g.Expect(i).ToNot(BeNil())
				g.Expect(i.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name).To(Equal("http"))
				g.Expect(i.Spec.Rules[0].HTTP.Paths[0].Path).To(Equal("/argo"))
				g.Expect(i.Spec.IngressClassName).To(Equal(ptr.To("nginx")))
				g.Expect(i.Annotations).To(HaveKeyWithValue("example.com/custom", "yes"))
				g.Expect(i.Annotations).To(HaveKeyWithValue(backendProtocolKey, "HTTP"), "defaults must survive custom annotations")
				g.Expect(i.Annotations).To(HaveKeyWithValue("cert-manager.io/issuer", "external"), "foreign annotations must be kept")
			}, "2m", "2s").Should(Succeed())

			By("updating the gRPC ingress through the CR")
			argocdFixture.Update(argoCD, func(a *argov1beta1api.ArgoCD) {
				a.Spec.Server.GRPC.Ingress.IngressClassName = ptr.To("nginx")
				a.Spec.Server.GRPC.Ingress.Annotations = map[string]string{"example.com/grpc": "yes"}
			})
			Eventually(func(g Gomega) {
				i := getIngress(grpcIngress.Name, ns.Name)
				g.Expect(i).ToNot(BeNil())
				g.Expect(i.Spec.IngressClassName).To(Equal(ptr.To("nginx")))
				g.Expect(i.Annotations).To(HaveKeyWithValue("example.com/grpc", "yes"))
				g.Expect(i.Annotations).To(HaveKeyWithValue(backendProtocolKey, "GRPC"))
			}, "2m", "2s").Should(Succeed())

			By("disabling both ingresses deletes them")
			argocdFixture.Update(argoCD, func(a *argov1beta1api.ArgoCD) {
				a.Spec.Server.Ingress.Enabled = false
				a.Spec.Server.GRPC.Ingress.Enabled = false
			})
			Eventually(serverIngress, "2m", "2s").Should(k8sFixture.NotExistByName())
			Eventually(grpcIngress, "2m", "2s").Should(k8sFixture.NotExistByName())
		})

		It("does not modify or delete an Ingress it does not own", Label("xks"), func() {

			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			By("creating a user-owned Ingress with the name the operator would use")
			pathType := networkingv1.PathTypeImplementationSpecific
			userIngress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "example-argocd-server",
					Namespace:   ns.Name,
					Annotations: map[string]string{"owner": "user"},
				},
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{{
						Host: "user.example.com",
						IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{{
								Path:     "/",
								PathType: &pathType,
								Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
									Name: "user-svc", Port: networkingv1.ServiceBackendPort{Number: 80}}},
							}},
						}},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, userIngress)).To(Succeed())

			By("creating Argo CD with server ingress enabled")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Server: argov1beta1api.ArgoCDServerSpec{Ingress: argov1beta1api.ArgoCDIngressSpec{Enabled: true}},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			unchanged := func(g Gomega) {
				i := getIngress(userIngress.Name, ns.Name)
				g.Expect(i).ToNot(BeNil(), "unowned ingress must not be deleted")
				g.Expect(i.Annotations).To(Equal(map[string]string{"owner": "user"}))
				g.Expect(i.Spec.Rules[0].Host).To(Equal("user.example.com"))
			}
			By("verifying the Ingress is left untouched while enabled")
			Consistently(unchanged, "30s", "3s").Should(Succeed())

			By("verifying the Ingress survives disabling ingress in the CR")
			argocdFixture.Update(argoCD, func(a *argov1beta1api.ArgoCD) { a.Spec.Server.Ingress.Enabled = false })
			Consistently(unchanged, "30s", "3s").Should(Succeed())
		})
	})
})
