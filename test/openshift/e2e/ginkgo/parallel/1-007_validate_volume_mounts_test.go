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

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-007_validate_volume_mounts", func() {

		var (
			ctx         context.Context
			k8sClient   client.Client
			randomNS    *corev1.Namespace
			cleanupFunc func()
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()

		})

		AfterEach(func() {
			fixture.OutputDebugOnFail(randomNS)

			if cleanupFunc != nil {
				cleanupFunc()
			}

		})

		It("verifies that applicationset controller has the expected volumes and volumemounts, including custom volumes and voluemmounts", func() {

			By("creating new namespace-scoped Argo CD instance with applicationset enabled, and custom volumes and volume mounts")
			randomNS, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			// The first part of this test is exactly the same as 1-019, so I have only ported the second part.

			argoCDRandomNS := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: randomNS.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					ApplicationSet: &argov1beta1api.ArgoCDApplicationSet{
						Volumes: []corev1.Volume{
							{Name: "empty-dir-volume", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "empty-dir-volume", MountPath: "/etc/test"},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCDRandomNS)).To(Succeed())

			By("verifying Argo CD and appset controller are running as expected")
			Eventually(argoCDRandomNS, "5m", "5s").Should(And(argocdFixture.BeAvailable(), argocdFixture.HaveApplicationSetControllerStatus("Running")))

			By("verifying appset controller Deployment has the expected volume and volumemount values")
			appsetControllerDepl := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-applicationset-controller", Namespace: randomNS.Name}}

			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&appsetControllerDepl), &appsetControllerDepl)).To(Succeed())

			Expect(appsetControllerDepl.Spec.Template.Spec.Containers[0].VolumeMounts).To(Equal([]corev1.VolumeMount{
				{Name: "ssh-known-hosts", MountPath: "/app/config/ssh"},
				{Name: "tls-certs", MountPath: "/app/config/tls"},
				{Name: "gpg-keys", MountPath: "/app/config/gpg/source"},
				{Name: "gpg-keyring", MountPath: "/app/config/gpg/keys"},
				{Name: "tmp", MountPath: "/tmp"},
				{Name: "empty-dir-volume", MountPath: "/etc/test"},
			}))

			Expect(appsetControllerDepl.Spec.Template.Spec.Volumes).To(Equal([]corev1.Volume{
				{
					Name: "ssh-known-hosts", VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							DefaultMode:          ptr.To(int32(420)),
							LocalObjectReference: corev1.LocalObjectReference{Name: "argocd-ssh-known-hosts-cm"}},
					},
				},
				{
					Name: "tls-certs", VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							DefaultMode:          ptr.To(int32(420)),
							LocalObjectReference: corev1.LocalObjectReference{Name: "argocd-tls-certs-cm"}},
					},
				},
				{
					Name: "gpg-keys", VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							DefaultMode: ptr.To(int32(420)),
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "argocd-gpg-keys-cm",
							},
						},
					},
				},
				{
					Name: "gpg-keyring", VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "tmp", VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "empty-dir-volume", VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			}))

		})

		It("verifies that dex controller has the expected volumes and volumemounts, including custom volumes and volumemounts", func() {

			By("creating new namespace-scoped Argo CD instance with dex enabled and custom volumes and volume mounts")
			randomNS, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			argoCDRandomNS := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: randomNS.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					SSO: &argov1beta1api.ArgoCDSSOSpec{
						Provider: argov1beta1api.SSOProviderTypeDex,
						Dex: &argov1beta1api.ArgoCDDexSpec{
							OpenShiftOAuth: true,
							Volumes: []corev1.Volume{
								{Name: "custom-dex-volume", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "custom-dex-volume", MountPath: "/custom/dex"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCDRandomNS)).To(Succeed())

			By("verifying Argo CD is available and Dex is running")
			Eventually(argoCDRandomNS, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying dex server Deployment has the expected volume and volumemount values")
			dexServerDepl := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-dex-server", Namespace: randomNS.Name}}

			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&dexServerDepl), &dexServerDepl)).To(Succeed())

			// Check that custom volume mounts are present
			Expect(dexServerDepl.Spec.Template.Spec.Containers[0].VolumeMounts).To(Equal([]corev1.VolumeMount{
				{Name: "static-files", MountPath: "/shared"},
				{Name: "dexconfig", MountPath: "/tmp"},
				{Name: "custom-dex-volume", MountPath: "/custom/dex"},
			}))

			// Verify that the deployment has the expected volumes (including custom ones)
			Expect(dexServerDepl.Spec.Template.Spec.Volumes).To(Equal([]corev1.Volume{
				{
					Name: "static-files",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "dexconfig",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "custom-dex-volume",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			}))
		})

	})
})
