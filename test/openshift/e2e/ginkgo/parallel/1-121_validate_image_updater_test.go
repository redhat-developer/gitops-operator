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
	"strings"

	appv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	imageUpdaterApi "github.com/argoproj-labs/argocd-image-updater/api/v1alpha1"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	applicationFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deplFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	osFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/os"
	ssFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-121_validate_image_updater_test", func() {

		var (
			k8sClient    client.Client
			ctx          context.Context
			ns           *corev1.Namespace
			cleanupFunc  func()
			imageUpdater *imageUpdaterApi.ImageUpdater
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		AfterEach(func() {
			if imageUpdater != nil {
				By("deleting ImageUpdater CR")
				Expect(k8sClient.Delete(ctx, imageUpdater)).To(Succeed())
				Eventually(imageUpdater).Should(k8sFixture.NotExistByName())
			}

			if cleanupFunc != nil {
				cleanupFunc()
			}

			fixture.OutputDebugOnFail(ns)

		})

		It("ensures that Image Updater will update Argo CD Application to the latest image", func() {

			By("creating simple namespace-scoped Argo CD instance with image updater enabled")
			ns, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			By("ensuring default service account has anyuid SCC permission")
			serviceAccountUser := "system:serviceaccount:" + ns.Name + ":default"
			output, err := osFixture.ExecCommand("oc", "auth", "can-i", "use", "scc/anyuid", "--as", serviceAccountUser)
			hasPermission := false
			if err == nil && len(output) > 0 {
				// Check if the service account user is already in the users list
				// Remove quotes and whitespace for comparison
				output = strings.TrimSpace(strings.Trim(output, "'\""))
				if strings.Contains(output, serviceAccountUser) {
					hasPermission = true
				}
			}
			if !hasPermission {
				_, err := osFixture.ExecCommand("oc", "adm", "policy", "add-scc-to-user", "anyuid", "-z", "default", "-n", ns.Name)
				Expect(err).NotTo(HaveOccurred(), "Failed to add anyuid SCC to default service account")
			}

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					ImageUpdater: argov1beta1api.ArgoCDImageUpdaterSpec{
						Env: []corev1.EnvVar{
							{
								Name:  "IMAGE_UPDATER_LOGLEVEL",
								Value: "trace",
							},
						},
						Enabled: true},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying all workloads are started")
			deploymentsShouldExist := []string{"argocd-redis", "argocd-server", "argocd-repo-server", "argocd-argocd-image-updater-controller"}
			for _, deplName := range deploymentsShouldExist {
				depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deplName, Namespace: ns.Name}}
				Eventually(depl, "2m", "5s").Should(k8sFixture.ExistByName(), "Deployment "+deplName+" did not exist within timeout")
				Eventually(depl, "2m", "5s").Should(deplFixture.HaveReplicas(1), "Deployment "+deplName+" did not have correct replicas within timeout")
				Eventually(depl, "3m", "5s").Should(deplFixture.HaveReadyReplicas(1), "Deployment "+deplName+" was not ready within timeout")
			}

			statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller", Namespace: ns.Name}}
			Eventually(statefulSet).Should(k8sFixture.ExistByName())
			Eventually(statefulSet).Should(ssFixture.HaveReplicas(1))
			Eventually(statefulSet, "3m", "5s").Should(ssFixture.HaveReadyReplicas(1))

			By("listing deployments in namespace for debugging")
			output, err = osFixture.ExecCommand("oc", "get", "deployments", "-n", ns.Name)
			if err == nil {
				GinkgoWriter.Printf("Deployments in namespace %s:\n%s\n", ns.Name, output)
			}

			By("creating Application")
			app := &appv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-01",
					Namespace: ns.Name,
				},
				Spec: appv1alpha1.ApplicationSpec{
					Project: "default",
					Source: &appv1alpha1.ApplicationSource{
						RepoURL:        "https://github.com/argoproj-labs/argocd-image-updater/",
						Path:           "test/e2e/testdata/005-public-guestbook",
						TargetRevision: "HEAD",
					},
					Destination: appv1alpha1.ApplicationDestination{
						Server:    "https://kubernetes.default.svc",
						Namespace: ns.Name,
					},
					SyncPolicy: &appv1alpha1.SyncPolicy{Automated: &appv1alpha1.SyncPolicyAutomated{}},
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			By("verifying deploying the Application succeeded")
			Eventually(app, "8m", "10s").Should(applicationFixture.HaveHealthStatusCode(health.HealthStatusHealthy), "Application did not reach healthy status within timeout")
			Eventually(app, "8m", "10s").Should(applicationFixture.HaveSyncStatusCode(appv1alpha1.SyncStatusCodeSynced), "Application did not sync within timeout")

			By("ensuring ImageUpdater controller deployment is ready before creating ImageUpdater CR")
			imageUpdaterDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-image-updater-controller", Namespace: ns.Name}}
			Eventually(imageUpdaterDepl, "3m", "5s").Should(deplFixture.HaveReadyReplicas(1), "ImageUpdater controller deployment was not ready within timeout")

			By("creating ImageUpdater CR")
			updateStrategy := "semver"
			imageUpdater = &imageUpdaterApi.ImageUpdater{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "image-updater",
					Namespace: ns.Name,
				},
				Spec: imageUpdaterApi.ImageUpdaterSpec{
					Namespace: ns.Name,
					ApplicationRefs: []imageUpdaterApi.ApplicationRef{
						{
							NamePattern: "app*",
							Images: []imageUpdaterApi.ImageConfig{
								{
									Alias:     "guestbook",
									ImageName: "quay.io/dkarpele/my-guestbook:~29437546.0",
									CommonUpdateSettings: &imageUpdaterApi.CommonUpdateSettings{
										UpdateStrategy: &updateStrategy,
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, imageUpdater)).To(Succeed())

			By("listing deployments in namespace after creating ImageUpdater CR")
			output, err = osFixture.ExecCommand("oc", "get", "deployments", "-n", ns.Name)
			if err == nil {
				GinkgoWriter.Printf("Deployments in namespace %s after ImageUpdater CR creation:\n%s\n", ns.Name, output)
			}

			By("checking ImageUpdater CR and Application status for debugging")
			output, err = osFixture.ExecCommand("oc", "get", "imageupdater", "image-updater", "-n", ns.Name, "-o", "yaml")
			if err == nil {
				GinkgoWriter.Printf("ImageUpdater CR status:\n%s\n", output)
			}

			output, err = osFixture.ExecCommand("oc", "get", "application", "app-01", "-n", ns.Name, "-o", "yaml")
			if err == nil {
				GinkgoWriter.Printf("Application status before waiting for image update:\n%s\n", output)
			}

			By("ensuring that the Application image has `29437546.0` version after update")
			Eventually(func() string {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(app), app)

				if err != nil {
					GinkgoWriter.Printf("Error getting Application: %v\n", err)
					return "" // Let Eventually retry on error
				}

				// Nil-safe check: The Kustomize block is only added by the Image Updater after its first run.
				// We must check that it and its Images field exist before trying to access them.
				if app.Spec.Source.Kustomize != nil && len(app.Spec.Source.Kustomize.Images) > 0 {
					imageStr := string(app.Spec.Source.Kustomize.Images[0])
					GinkgoWriter.Printf("Found Kustomize image: %s\n", imageStr)
					return imageStr
				}

				// Return an empty string to signify the condition is not yet met.
				GinkgoWriter.Printf("Application Kustomize block not yet updated. Kustomize: %v\n", app.Spec.Source.Kustomize)
				return ""
			}, "10m", "10s").Should(Equal("quay.io/dkarpele/my-guestbook:29437546.0"), "Image Updater did not update the Application image within timeout")
		})
	})
})
