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
	"os"
	"strings"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	"github.com/argoproj/argo-cd/gitops-engine/pkg/health"
	argocdv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	appFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	configmapFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/configmap"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-132_validate_sensitive_annotation_masking_test", func() {

		// tokenAnnotationKey is the OpenShift annotation that carries a service-account
		// token in plain text on secrets of type kubernetes.io/dockercfg.
		const tokenAnnotationKey = "openshift.io/token-secret.value"

		// sensitiveToken is a fake token that represents the kind of value OpenShift
		// stores in the annotation.  It must NOT appear in argocd app diff output.
		const sensitiveToken = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.fake-openshift-service-account-token"

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that resource.sensitive.mask.annotations is set in argocd-cm and that the openshift.io/token-secret.value annotation is hidden from diff computation so the app stays Synced and the token is never visible in CLI output", func() {

			fixture.EnsureRunningOnOpenShift()

			By("creating namespace for the ArgoCD instance")
			argoCDNS, cleanupArgoCDNS := fixture.CreateNamespaceWithCleanupFunc("test-1-132-argocd")
			defer cleanupArgoCDNS()

			By("creating a namespace-scoped ArgoCD instance with the server Route enabled")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: argoCDNS.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Server: argov1beta1api.ArgoCDServerSpec{
						Route: argov1beta1api.ArgoCDRouteSpec{Enabled: true},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD instance to be available")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying argocd-cm has resource.sensitive.mask.annotations set to openshift.io/token-secret.value")
			argocdCM := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-cm", Namespace: argoCDNS.Name}}
			Eventually(argocdCM).Should(k8sFixture.ExistByName())
			Eventually(argocdCM).Should(configmapFixture.HaveStringDataKeyValue("resource.sensitive.mask.annotations", tokenAnnotationKey))

			By("creating a managed namespace for app deployment")
			appNS, cleanupAppNS := fixture.CreateManagedNamespaceWithCleanupFunc("test-1-132-apps", argoCDNS.Name)
			defer cleanupAppNS()

			By("creating a per-test ArgoCD CLI config file to prevent parallel-test login context conflicts")
			cliConfigFile, err := os.CreateTemp("", "argocd-e2e-1-132-*.yaml")
			Expect(err).ToNot(HaveOccurred())
			cliConfigFile.Close()
			defer os.Remove(cliConfigFile.Name())

			By("waiting for the ArgoCD server Route to be admitted and assigned a host")
			argoCDRoute := &routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "argocd-server", Namespace: argoCDNS.Name}}
			Eventually(argoCDRoute).Should(k8sFixture.ExistByName())
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCDRoute), argoCDRoute); err != nil {
					return false
				}
				return argoCDRoute.Spec.Host != "" ||
					(len(argoCDRoute.Status.Ingress) > 0 && argoCDRoute.Status.Ingress[0].Host != "")
			}, "2m", "5s").Should(BeTrue())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCDRoute), argoCDRoute)).To(Succeed())
			routeHost := argoCDRoute.Spec.Host
			if routeHost == "" {
				routeHost = argoCDRoute.Status.Ingress[0].Host
			}

			By("reading the admin password from the ArgoCD cluster secret")
			adminSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "argocd-cluster", Namespace: argoCDNS.Name}}
			Eventually(adminSecret).Should(k8sFixture.ExistByName())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(adminSecret), adminSecret)).To(Succeed())
			adminPassword := string(adminSecret.Data["admin.password"])

			By("logging in to the namespace-scoped ArgoCD instance via CLI with a per-test config file")
			Eventually(func() bool {
				output, loginErr := argocdFixture.RunArgoCDCLI(
					"login", routeHost,
					"--config", cliConfigFile.Name(),
					"--username", "admin",
					"--password", adminPassword,
					"--insecure",
					"--skip-test-tls",
				)
				if loginErr != nil {
					GinkgoWriter.Println("CLI login error:", loginErr, "output:", output)
					return false
				}
				return strings.Contains(output, "logged in successfully")
			}, "3m", "10s").Should(BeTrue())

			By("creating an ArgoCD Application that deploys a kubernetes.io/dockercfg secret (the git source does NOT include the sensitive annotation)")
			app := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "dockercfg-test-app", Namespace: argoCDNS.Name},
				Spec: argocdv1alpha1.ApplicationSpec{
					Project: "default",
					Source: &argocdv1alpha1.ApplicationSource{
						RepoURL:        "https://github.com/redhat-developer/gitops-operator",
						Path:           "test/examples/dockercfg-token-secret",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Server:    "https://kubernetes.default.svc",
						Namespace: appNS.Name,
					},
					SyncPolicy: &argocdv1alpha1.SyncPolicy{
						Automated: &argocdv1alpha1.SyncPolicyAutomated{
							// SelfHeal is disabled to make the test deterministic: without it
							// ArgoCD would still auto-sync (reverting the annotation) on the
							// next periodic cycle before the Consistently check completes.
							SelfHeal: ptr.To(false),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			By("waiting for the app to sync and the dockercfg secret to be deployed")
			Eventually(app, "5m", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app, "5m", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

			By("simulating OpenShift behavior: adding openshift.io/token-secret.value to the live secret")
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "dockercfg-token-secret", Namespace: appNS.Name}}
			Eventually(secret).Should(k8sFixture.ExistByName())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)).To(Succeed())
			if secret.Annotations == nil {
				secret.Annotations = map[string]string{}
			}
			secret.Annotations[tokenAnnotationKey] = sensitiveToken
			Expect(k8sClient.Update(ctx, secret)).To(Succeed())

			By("forcing a single ArgoCD refresh so the live state is re-evaluated")
			// --grpc-web suppresses the gRPC-over-HTTP2 warning emitted by newer ArgoCD CLIs
			_, refreshErr := argocdFixture.RunArgoCDCLI(
				"--config", cliConfigFile.Name(),
				"app", "get", "dockercfg-test-app",
				"--refresh", "--insecure", "--grpc-web",
			)
			Expect(refreshErr).NotTo(HaveOccurred())

			By("verifying the app STAYS Synced even though the annotation is present on the live secret")
			// resource.sensitive.mask.annotations strips the annotation from the normalized
			// manifest before diff computation, so the live drift is invisible to ArgoCD.
			// The app must never transition to OutOfSync – that is the feature under test.
			Consistently(app, "30s", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

			By("running argocd app diff and verifying the sensitive token value is not visible in the output")
			// argocd app diff exits 0 when there are no differences and 1 when differences exist.
			// Asserting no error therefore also proves ArgoCD computed an empty diff, which
			// is the expected outcome when the annotation is properly masked.
			diffOutput, diffErr := argocdFixture.RunArgoCDCLI(
				"--config", cliConfigFile.Name(),
				"app", "diff", "dockercfg-test-app",
				"--insecure", "--grpc-web",
			)
			GinkgoWriter.Println("argocd app diff output:", diffOutput)
			Expect(diffErr).NotTo(HaveOccurred(), diffOutput)
			Expect(diffOutput).NotTo(
				ContainSubstring(sensitiveToken),
				"sensitive token value must not appear in argocd app diff output",
			)
		})
	})
})
