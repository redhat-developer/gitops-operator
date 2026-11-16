/*
Copyright 2026.

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
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argov1beta1api "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	routeFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/route"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
)

// quayWebhookPayload is a minimal Quay repository push notification, as parsed by the Image Updater Quay handler.
const quayWebhookPayload = `{"repository":"argoprojlabs/argocd-image-updater","docker_url":"quay.io/argoprojlabs/argocd-image-updater","updated_tags":["e2e"]}`

// expectImageUpdaterWebhookResponse posts a Quay webhook through the Route at host, and waits until the response has the
// expected status code and its body contains expectedBody. The response bodies checked by callers are written by
// Image Updater itself, so a match proves the request travelled through the router to the webhook server.
func expectImageUpdaterWebhookResponse(host, secret string, expectedStatus int, expectedBody string) {
	query := url.Values{"type": {"quay.io"}, "secret": {secret}}
	webhookURL := "https://" + host + "/webhook?" + query.Encode()
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 // e2e test against operator-managed routes
		},
	}

	Eventually(func(g Gomega) {
		resp, err := httpClient.Post(webhookURL, "application/json", bytes.NewBufferString(quayWebhookPayload))
		g.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		g.Expect(err).NotTo(HaveOccurred())
		GinkgoWriter.Printf("image updater webhook response: %d %q\n", resp.StatusCode, string(body))
		g.Expect(resp.StatusCode).To(Equal(expectedStatus))
		g.Expect(string(body)).To(ContainSubstring(expectedBody))
	}, "3m", "5s").Should(Succeed())
}

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-144_validate_image_updater_webhook", func() {

		var (
			ctx         context.Context
			k8sClient   client.Client
			ns          *corev1.Namespace
			cleanupFunc func()
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		AfterEach(func() {
			// Output debug information before the namespace is deleted, so that it includes the namespace contents.
			fixture.OutputDebugOnFail(ns)

			if cleanupFunc != nil {
				cleanupFunc()
			}
		})

		It("ensures that the Image Updater webhook Service, Route and pod configuration follow the ArgoCD CR", Label("openshift"), func() {

			fixture.EnsureRunningOnOpenShift()

			ns, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			By("creating a Secret holding the Quay webhook secret")
			quaySecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "quay-webhook", Namespace: ns.Name},
				StringData: map[string]string{"secret": "my-quay-secret"},
			}
			Expect(k8sClient.Create(ctx, quaySecret)).To(Succeed())

			By("creating Argo CD with the Image Updater webhook exposed through a Route")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					ImageUpdater: argov1beta1api.ArgoCDImageUpdaterSpec{
						Enabled: true,
						WebhookServer: argov1beta1api.ImageUpdaterWebhookServerSpec{
							Route: argov1beta1api.ArgoCDRouteSpec{Enabled: true},
							Secrets: &argov1beta1api.ImageUpdaterWebhookSecretsSpec{
								Quay: &argov1beta1api.WebhookSecretKeySelector{Name: quaySecret.Name, Key: "secret"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying the webhook Service requests a serving certificate and exposes the webhook port")
			svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "example-image-updater-webhook", Namespace: ns.Name}}
			Eventually(svc).Should(k8sFixture.ExistByName())
			Eventually(svc).Should(k8sFixture.HaveAnnotationWithValue("service.beta.openshift.io/serving-cert-secret-name", "example-image-updater-webhook-tls"))
			Expect(svc.Spec.Ports).To(HaveLen(1))
			Expect(svc.Spec.Ports[0].Name).To(Equal("webhook"))
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(8082)))
			Expect(svc.Spec.Selector).To(HaveKeyWithValue("app.kubernetes.io/name", "example-argocd-image-updater-controller"))

			By("verifying the serving certificate is issued")
			tlsSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "example-image-updater-webhook-tls", Namespace: ns.Name}}
			Eventually(tlsSecret, "2m", "5s").Should(k8sFixture.ExistByName())

			By("verifying the webhook Route defaults to reencrypt termination")
			route := &routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "example-image-updater-webhook", Namespace: ns.Name}}
			Eventually(route).Should(k8sFixture.ExistByName())
			Eventually(route).Should(routeFixture.HaveTLS(routev1.TLSTerminationReencrypt, routev1.InsecureEdgeTerminationPolicyRedirect))
			Eventually(route).Should(routeFixture.HavePort(intstr.FromString("webhook")))
			Eventually(route, "2m", "5s").Should(routeFixture.HaveAdmittedIngress())
			Expect(route.Spec.Path).To(Equal("/webhook"))
			Expect(route.Spec.To.Name).To(Equal("example-image-updater-webhook"))

			By("verifying the Image Updater deployment enables the webhook server with TLS")
			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "example-argocd-image-updater-controller", Namespace: ns.Name}}
			Eventually(depl).Should(k8sFixture.ExistByName())
			Eventually(depl).Should(deploymentFixture.HaveContainerWithEnvVar("ENABLE_WEBHOOK", "true", 0))
			webhookTLSVolume := corev1.Volume{
				Name: "webhook-tls",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName:  "example-image-updater-webhook-tls",
						Optional:    new(true),
						DefaultMode: new(int32(420)),
					},
				},
			}
			Eventually(depl).Should(deploymentFixture.HaveSpecTemplateSpecVolume(webhookTLSVolume))

			By("verifying the Quay webhook secret is referenced from the user Secret")
			Eventually(func() *corev1.EnvVarSource {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(depl), depl); err != nil {
					return nil
				}
				container := deploymentFixture.GetTemplateSpecContainerByName("argocd-image-updater-controller", *depl)
				if container == nil {
					return nil
				}
				for _, env := range container.Env {
					if env.Name == "QUAY_WEBHOOK_SECRET" {
						return env.ValueFrom
					}
				}
				return nil
			}).Should(Equal(&corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: quaySecret.Name},
					Key:                  "secret",
					Optional:             new(true),
				},
			}))

			// The router only reaches the webhook server through a reencrypt Route once the pod serves the
			// service CA certificate. If the pod started before the certificate was issued and was not rolled
			// afterwards, it serves a self-signed certificate and the router answers with its own 503 page.
			By("verifying the webhook server rejects a Quay webhook with the wrong secret")
			expectImageUpdaterWebhookResponse(route.Spec.Host, "wrong-secret", http.StatusBadRequest, "invalid webhook secret")

			By("verifying the webhook server accepts a Quay webhook with the configured secret")
			expectImageUpdaterWebhookResponse(route.Spec.Host, "my-quay-secret", http.StatusOK, "Webhook received and processing")

			By("verifying no webhook configuration condition is reported")
			Consistently(func() []metav1.Condition {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCD), argoCD); err != nil {
					return nil
				}
				return argoCD.Status.Conditions
			}, "15s", "5s").ShouldNot(ContainElement(HaveField("Type", argov1beta1api.ArgoCDConditionImageUpdaterWebhookConfigurationError)))

			By("switching the webhook server to insecure mode")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ImageUpdater.WebhookServer.Insecure = true
			})

			Eventually(route).Should(routeFixture.HaveTLS(routev1.TLSTerminationEdge, routev1.InsecureEdgeTerminationPolicyRedirect))
			Eventually(depl).Should(deploymentFixture.HaveContainerWithEnvVar("DISABLE_TLS", "true", 0))
			Eventually(depl).ShouldNot(deploymentFixture.HaveSpecTemplateSpecVolume(webhookTLSVolume))
			Eventually(svc).ShouldNot(k8sFixture.HaveAnnotationWithValue("service.beta.openshift.io/serving-cert-secret-name", "example-image-updater-webhook-tls"))

			By("verifying the plain HTTP webhook server accepts a Quay webhook through the edge Route")
			expectImageUpdaterWebhookResponse(route.Spec.Host, "my-quay-secret", http.StatusOK, "Webhook received and processing")

			By("setting a Route TLS termination that contradicts insecure mode")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ImageUpdater.WebhookServer.Route.TLS = &routev1.TLSConfig{Termination: routev1.TLSTerminationReencrypt}
			})
			Eventually(func() []metav1.Condition {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCD), argoCD); err != nil {
					return nil
				}
				return argoCD.Status.Conditions
			}, "2m", "5s").Should(ContainElement(And(
				HaveField("Type", argov1beta1api.ArgoCDConditionImageUpdaterWebhookConfigurationError),
				HaveField("Status", metav1.ConditionTrue),
			)))

			By("disabling the webhook Route")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ImageUpdater.WebhookServer.Route = argov1beta1api.ArgoCDRouteSpec{Enabled: false}
			})

			Eventually(route).Should(k8sFixture.NotExistByName())
			Eventually(svc).Should(k8sFixture.NotExistByName())
			Eventually(depl).ShouldNot(deploymentFixture.HaveContainerWithEnvVar("ENABLE_WEBHOOK", "true", 0))
			Eventually(func() []metav1.Condition {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCD), argoCD); err != nil {
					return nil
				}
				return argoCD.Status.Conditions
			}, "2m", "5s").ShouldNot(ContainElement(HaveField("Type", argov1beta1api.ArgoCDConditionImageUpdaterWebhookConfigurationError)))
		})
	})
})
