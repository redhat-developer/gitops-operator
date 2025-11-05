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
	"fmt"
	"strings"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-098_validate_dex_clientsecret_deprecated", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("validates that dex client secret is properly copied from service account token to argocd-secret", func() {

			// Create namespace for this test and ensure cleanup
			namespace, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			By("creating ArgoCD CR with dex SSO enabled using openShiftOAuth")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-argocd",
					Namespace: namespace.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{
					SSO: &argov1beta1api.ArgoCDSSOSpec{
						Provider: argov1beta1api.SSOProviderTypeDex,
						Dex: &argov1beta1api.ArgoCDDexSpec{
							OpenShiftOAuth: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("verifying ArgoCD instance reaches Available phase")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying dex server service account exists")
			dexServiceAccount := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-argocd-argocd-dex-server",
					Namespace: namespace.Name,
				},
			}
			Eventually(dexServiceAccount, "2m", "5s").Should(k8sFixture.ExistByName())

			By("validating that the Dex Client Secret was copied from dex serviceaccount token secret to argocd-secret, by the operator")
			Eventually(func() error {
				// Get the service account and find its token secret
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dexServiceAccount), dexServiceAccount)
				if err != nil {
					return err
				}

				// Find the token secret from the service account secrets
				var tokenSecretName string
				for _, secret := range dexServiceAccount.Secrets {
					if secret.Name != "" && strings.Contains(secret.Name, "token") {
						tokenSecretName = secret.Name
						break
					}
				}

				if tokenSecretName == "" {
					return fmt.Errorf("no token secret found for service account %s", dexServiceAccount.Name)
				}

				// Get the token secret and extract the token
				tokenSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      tokenSecretName,
						Namespace: namespace.Name,
					},
				}
				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(tokenSecret), tokenSecret)
				if err != nil {
					return err
				}

				expectedClientSecret, exists := tokenSecret.Data["token"]
				if !exists {
					return fmt.Errorf("token not found in secret %s", tokenSecretName)
				}

				// Get the argocd-secret and extract the oidc.dex.clientSecret
				argoCDSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "argocd-secret",
						Namespace: namespace.Name,
					},
				}
				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCDSecret), argoCDSecret)
				if err != nil {
					return err
				}

				actualClientSecret, exists := argoCDSecret.Data["oidc.dex.clientSecret"]
				if !exists {
					return fmt.Errorf("oidc.dex.clientSecret not found in argocd-secret")
				}

				// Compare the two secrets
				if string(expectedClientSecret) != string(actualClientSecret) {
					return fmt.Errorf("dex client secret mismatch: expected length %d, actual length %d",
						len(expectedClientSecret), len(actualClientSecret))
				}

				return nil
			}, "3m", "5s").Should(Succeed())

		})

	})
})
