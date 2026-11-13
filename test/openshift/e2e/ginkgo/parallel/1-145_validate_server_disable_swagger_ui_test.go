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
	"context"
	"errors"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	argov1beta1api "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-145_validate_server_disable_swagger_ui", func() {

		var (
			k8sClient client.Client
			clientset kubernetes.Interface
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			clientset = fixtureUtils.GetE2ETestKubeClientset()
			ctx = context.Background()
		})

		It("verifies that the Argo CD server stops serving the Swagger UI when server.disable.swagger.ui is set in .spec.cmdParams", func() {

			By("creating a namespace-scoped Argo CD instance with the default configuration")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			// serverStatusCode requests the path from the Argo CD server, through the API server proxy to the server
			// Service, so that only the Pods that are ready serve the request. It returns 0 if the request itself fails.
			serverStatusCode := func(path string) func() int {
				return func() int {
					// Bound each request, so that one that hangs while the server Pod is replaced is retried.
					requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
					defer cancel()

					result := clientset.CoreV1().RESTClient().Get().
						Namespace(ns.Name).
						Resource("services").
						Name("https:argocd-server:https").
						SubResource("proxy").
						Suffix(path).
						Do(requestCtx)

					var statusCode int
					result.StatusCode(&statusCode)
					// The result of a failed request only has the status code when the response is an API object,
					// which the plain text responses of the Argo CD server are not: read it from the error instead.
					var statusErr *apierrors.StatusError
					if errors.As(result.Error(), &statusErr) {
						statusCode = int(statusErr.ErrStatus.Code)
					}
					GinkgoWriter.Println("GET", path, "from argocd-server:", statusCode)
					return statusCode
				}
			}

			By("verifying that the Swagger UI and the OpenAPI specification are served by default")
			Eventually(serverStatusCode("healthz"), "2m", "5s").Should(Equal(http.StatusOK))
			Eventually(serverStatusCode("swagger-ui"), "2m", "5s").Should(Equal(http.StatusOK))
			Eventually(serverStatusCode("swagger.json"), "2m", "5s").Should(Equal(http.StatusOK))

			By("setting server.disable.swagger.ui in .spec.cmdParams")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.CmdParams = map[string]string{"server.disable.swagger.ui": "true"}
			})

			By("verifying that the operator sets the parameter on the server Deployment, which rolls it out")
			serverDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-server", Namespace: ns.Name}}
			Eventually(serverDepl, "2m", "5s").Should(deploymentFixture.HaveContainerWithEnvVar("ARGOCD_SERVER_DISABLE_SWAGGER_UI", "true", 0))
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying that the Swagger UI and the OpenAPI specification are no longer served")
			// Without the Swagger UI handler, the server answers /swagger-ui as any other unknown path: 404, unless
			// the client accepts 'text/html', when it is served the Argo CD UI instead. This client does not.
			Eventually(serverStatusCode("swagger-ui"), "5m", "5s").Should(Equal(http.StatusNotFound))
			Eventually(serverStatusCode("swagger.json"), "5m", "5s").Should(Equal(http.StatusNotFound))

			By("verifying that the server still answers other requests, so the 404 is not caused by the server being unreachable")
			Eventually(serverStatusCode("healthz"), "2m", "5s").Should(Equal(http.StatusOK))
		})
	})
})
