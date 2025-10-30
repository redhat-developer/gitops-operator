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

package sequential

import (
	"context"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-114_validate_imagepullpolicy", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that imagePullPolicy from ArgoCD CR spec is propagated to all ArgoCD workload resources", func() {

			By("creating a test namespace for ArgoCD instance")
			ns := fixture.CreateNamespace("test-1-114-imagepullpolicy")
			defer func() {
				Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
			}()

			By("creating an ArgoCD instance with imagePullPolicy set to Always")
			policy := corev1.PullAlways
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					ImagePullPolicy: &policy,
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, argoCD)).To(Succeed())
			}()

			By("waiting for ArgoCD to become available")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying all ArgoCD deployments have imagePullPolicy set to Always on all containers")
			deploymentNames := []string{
				"argocd-server",
				"argocd-repo-server",
				"argocd-redis",
			}

			for _, deplName := range deploymentNames {
				depl := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: deplName, Namespace: ns.Name},
				}
				Eventually(depl, "3m", "5s").Should(k8sFixture.ExistByName())
				Eventually(depl, "2m", "5s").Should(deployment.HaveReadyReplicas(1))

				// Verify all containers in the deployment have the correct imagePullPolicy
				Expect(depl.Spec.Template.Spec.Containers).ToNot(BeEmpty())
				for _, container := range depl.Spec.Template.Spec.Containers {
					Expect(container.ImagePullPolicy).To(Equal(corev1.PullAlways),
						"Deployment %s, container %s should have ImagePullPolicy set to Always",
						deplName, container.Name)
				}
			}

			By("verifying ArgoCD application-controller statefulset has imagePullPolicy set to Always")
			ss := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-application-controller",
					Namespace: ns.Name,
				},
			}
			Eventually(ss, "3m", "5s").Should(k8sFixture.ExistByName())
			Eventually(ss, "2m", "5s").Should(statefulsetFixture.HaveReadyReplicas(1))

			Expect(ss.Spec.Template.Spec.Containers).ToNot(BeEmpty())
			for _, container := range ss.Spec.Template.Spec.Containers {
				Expect(container.ImagePullPolicy).To(Equal(corev1.PullAlways),
					"StatefulSet argocd-application-controller, container %s should have ImagePullPolicy set to Always",
					container.Name)
			}

			By("updating ArgoCD instance to use imagePullPolicy IfNotPresent")
			policy = corev1.PullIfNotPresent
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ImagePullPolicy = &policy
			})

			By("waiting for ArgoCD to reconcile the change")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying all deployments have been updated to use IfNotPresent imagePullPolicy")
			for _, deplName := range deploymentNames {
				depl := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: deplName, Namespace: ns.Name},
				}
				Eventually(depl).Should(k8sFixture.ExistByName())

				// Eventually the imagePullPolicy should be updated
				Eventually(func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: deplName, Namespace: ns.Name}, depl)
					if err != nil {
						return false
					}
					if len(depl.Spec.Template.Spec.Containers) == 0 {
						return false
					}
					for _, container := range depl.Spec.Template.Spec.Containers {
						if container.ImagePullPolicy != corev1.PullIfNotPresent {
							return false
						}
					}
					return true
				}, "3m", "5s").Should(BeTrue(),
					"Deployment %s should have all containers with ImagePullPolicy set to IfNotPresent", deplName)
			}

			By("verifying statefulset has been updated to use IfNotPresent imagePullPolicy")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "argocd-application-controller", Namespace: ns.Name}, ss)
				if err != nil {
					return false
				}
				for _, container := range ss.Spec.Template.Spec.Containers {
					if container.ImagePullPolicy != corev1.PullIfNotPresent {
						return false
					}
				}
				return true
			}, "3m", "5s").Should(BeTrue(),
				"StatefulSet argocd-application-controller should have all containers with ImagePullPolicy set to IfNotPresent")

		})

		It("verifies that imagePullPolicy works correctly on default openshift-gitops ArgoCD instance", func() {

			openshiftGitopsArgoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			By("verifying that the openshift-gitops ArgoCD instance exists and is available")
			Eventually(openshiftGitopsArgoCD).Should(k8sFixture.ExistByName())
			Eventually(openshiftGitopsArgoCD).Should(argocdFixture.BeAvailable())

			By("updating openshift-gitops ArgoCD to set imagePullPolicy to Always")
			argocdFixture.Update(openshiftGitopsArgoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ImagePullPolicy = ptr.To(corev1.PullAlways)
			})

			defer func() {
				By("restoring openshift-gitops ArgoCD imagePullPolicy to default after test")
				argocdFixture.Update(openshiftGitopsArgoCD, func(ac *argov1beta1api.ArgoCD) {
					ac.Spec.ImagePullPolicy = nil
				})
			}()

			By("waiting for ArgoCD to reconcile")
			Eventually(openshiftGitopsArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying openshift-gitops deployments have imagePullPolicy set to Always")
			deploymentNames := []string{
				"openshift-gitops-server",
				"openshift-gitops-repo-server",
				"openshift-gitops-redis",
				"openshift-gitops-applicationset-controller",
			}

			for _, deplName := range deploymentNames {
				depl := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: deplName, Namespace: "openshift-gitops"},
				}
				Eventually(depl).Should(k8sFixture.ExistByName())

				Eventually(func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: deplName, Namespace: "openshift-gitops"}, depl)
					if err != nil {
						return false
					}
					if len(depl.Spec.Template.Spec.Containers) == 0 {
						return false
					}
					for _, container := range depl.Spec.Template.Spec.Containers {
						if container.ImagePullPolicy != corev1.PullAlways {
							return false
						}
					}
					return true
				}, "3m", "5s").Should(BeTrue(),
					"openshift-gitops Deployment %s should have all containers with ImagePullPolicy set to Always", deplName)
			}

			By("verifying openshift-gitops statefulset has imagePullPolicy set to Always")
			ss := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-application-controller",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(ss).Should(k8sFixture.ExistByName())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "openshift-gitops-application-controller", Namespace: "openshift-gitops"}, ss)
				if err != nil {
					return false
				}
				if len(ss.Spec.Template.Spec.Containers) == 0 {
					return false
				}
				for _, container := range ss.Spec.Template.Spec.Containers {
					if container.ImagePullPolicy != corev1.PullAlways {
						return false
					}
				}
				return true
			}, "3m", "5s").Should(BeTrue(),
				"openshift-gitops StatefulSet should have all containers with ImagePullPolicy set to Always")

		})

		It("verifies default imagePullPolicy is applied to all ArgoCD workload resources when not specified in either CR spec or subscription", func() {

			openshiftGitopsArgoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			By("verifying that the openshift-gitops ArgoCD instance exists and is available")
			Eventually(openshiftGitopsArgoCD).Should(k8sFixture.ExistByName())
			Eventually(openshiftGitopsArgoCD).Should(argocdFixture.BeAvailable())

			By("waiting for ArgoCD to reconcile")
			Eventually(openshiftGitopsArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying openshift-gitops deployments have imagePullPolicy set to default(IfNotPresent)")
			deploymentNames := []string{
				"openshift-gitops-server",
				"openshift-gitops-repo-server",
				"openshift-gitops-redis",
				"openshift-gitops-applicationset-controller",
			}

			for _, deplName := range deploymentNames {
				depl := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: deplName, Namespace: "openshift-gitops"},
				}
				Eventually(depl).Should(k8sFixture.ExistByName())

				Eventually(func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: deplName, Namespace: "openshift-gitops"}, depl)
					if err != nil {
						return false
					}
					if len(depl.Spec.Template.Spec.Containers) == 0 {
						return false
					}
					for _, container := range depl.Spec.Template.Spec.Containers {
						if container.ImagePullPolicy != corev1.PullIfNotPresent {
							return false
						}
					}
					return true
				}, "3m", "5s").Should(BeTrue(),
					"openshift-gitops Deployment %s should have all containers with ImagePullPolicy set to default(IfNotPresent)", deplName)
			}

			By("verifying openshift-gitops statefulset has imagePullPolicy set to default(IfNotPresent)")
			ss := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-application-controller",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(ss).Should(k8sFixture.ExistByName())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "openshift-gitops-application-controller", Namespace: "openshift-gitops"}, ss)
				if err != nil {
					return false
				}
				if len(ss.Spec.Template.Spec.Containers) == 0 {
					return false
				}
				for _, container := range ss.Spec.Template.Spec.Containers {
					if container.ImagePullPolicy != corev1.PullIfNotPresent {
						return false
					}
				}
				return true
			}, "3m", "5s").Should(BeTrue(),
				"openshift-gitops StatefulSet should have all containers with ImagePullPolicy set to default(PullIfNotPresent)")

		})

		It("verifies that IMAGE_PULL_POLICY environment variable at operator level sets default imagePullPolicy for all ArgoCD instances", func() {

			if fixture.EnvLocalRun() {
				Skip("Skipping test as LOCAL_RUN env var is set. In this case, it is not possible to set env var on gitops operator controller process.")
				return
			}

			By("creating a test namespace for first ArgoCD instance without imagePullPolicy set")
			ns1 := fixture.CreateNamespace("test-1-114-env-default")
			defer func() {
				Expect(k8sClient.Delete(ctx, ns1)).To(Succeed())
			}()

			By("creating an ArgoCD instance without explicitly setting imagePullPolicy")
			argoCD1 := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns1.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD1)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, argoCD1)).To(Succeed())
			}()

			By("waiting for ArgoCD to become available")
			Eventually(argoCD1, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("setting IMAGE_PULL_POLICY environment variable at operator level to Always")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("IMAGE_PULL_POLICY", "Always")

			defer func() {
				By("removing IMAGE_PULL_POLICY environment variable to restore default behavior")
				fixture.RestoreSubcriptionToDefault()
			}()

			By("verifying second ArgoCD deployments inherit operator-level imagePullPolicy (Always)")
			deploymentNames := []string{
				"argocd-server",
				"argocd-repo-server",
				"argocd-redis",
			}

			By("verifying operator has restarted with IMAGE_PULL_POLICY environment variable set")
			operatorControllerDepl := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-operator-controller-manager",
					Namespace: "openshift-gitops-operator",
				},
			}
			Eventually(operatorControllerDepl).Should(k8sFixture.ExistByName())
			Eventually(operatorControllerDepl).Should(deployment.HaveAvailableReplicas(1))
			Eventually(operatorControllerDepl).Should(deployment.HaveReadyReplicas(1))

			By("verifying first ArgoCD deployment has ImagePullPolicy set to Always")
			for _, deplName := range deploymentNames {
				depl := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: deplName, Namespace: ns1.Name},
				}
				Eventually(depl, "3m", "5s").Should(k8sFixture.ExistByName())

				Eventually(func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: deplName, Namespace: ns1.Name}, depl)
					if err != nil {
						return false
					}
					for _, container := range depl.Spec.Template.Spec.Containers {
						if container.ImagePullPolicy != corev1.PullAlways {
							return false
						}
					}
					return true
				}, "3m", "5s").Should(BeTrue(),
					"Deployment %s in namespace %s should inherit operator-level imagePullPolicy (Always)", deplName, ns1.Name)
			}

			By("verifying first ArgoCD statefulset inherits operator-level imagePullPolicy (Always)")
			ss1 := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-application-controller",
					Namespace: ns1.Name,
				},
			}
			Eventually(ss1, "3m", "5s").Should(k8sFixture.ExistByName())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "argocd-application-controller", Namespace: ns1.Name}, ss1)
				if err != nil {
					return false
				}
				if len(ss1.Spec.Template.Spec.Containers) == 0 {
					return false
				}
				for _, container := range ss1.Spec.Template.Spec.Containers {
					if container.ImagePullPolicy != corev1.PullAlways {
						return false
					}
				}
				return true
			}, "3m", "5s").Should(BeTrue(),
				"StatefulSet in namespace %s should inherit operator-level imagePullPolicy (Always)", ns1.Name)

			By("creating a second test namespace for ArgoCD instance that should inherit operator-level default")
			ns2 := fixture.CreateNamespace("test-1-114-env-inherit")
			defer func() {
				Expect(k8sClient.Delete(ctx, ns2)).To(Succeed())
			}()

			By("creating a second ArgoCD instance without explicitly setting imagePullPolicy")
			argoCD2 := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns2.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD2)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, argoCD2)).To(Succeed())
			}()

			By("waiting for second ArgoCD to become available")
			Eventually(argoCD2, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying second ArgoCD deployments inherits operator-level imagePullPolicy (Always)")
			for _, deplName := range deploymentNames {
				depl := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: deplName, Namespace: ns2.Name},
				}
				Eventually(depl, "3m", "5s").Should(k8sFixture.ExistByName())

				Eventually(func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: deplName, Namespace: ns2.Name}, depl)
					if err != nil {
						return false
					}
					for _, container := range depl.Spec.Template.Spec.Containers {
						if container.ImagePullPolicy != corev1.PullAlways {
							return false
						}
					}
					return true
				}, "3m", "5s").Should(BeTrue(),
					"Deployment %s in namespace %s should inherit operator-level imagePullPolicy (Always)", deplName, ns2.Name)
			}

			By("verifying second ArgoCD statefulset inherits operator-level imagePullPolicy (Always)")
			ss2 := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-application-controller",
					Namespace: ns2.Name,
				},
			}
			Eventually(ss2, "3m", "5s").Should(k8sFixture.ExistByName())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "argocd-application-controller", Namespace: ns2.Name}, ss2)
				if err != nil {
					return false
				}
				if len(ss2.Spec.Template.Spec.Containers) == 0 {
					return false
				}
				for _, container := range ss2.Spec.Template.Spec.Containers {
					if container.ImagePullPolicy != corev1.PullAlways {
						return false
					}
				}
				return true
			}, "3m", "5s").Should(BeTrue(),
				"StatefulSet in namespace %s should inherit operator-level imagePullPolicy (Always)", ns2.Name)

			By("creating a third test namespace for ArgoCD instance with explicit imagePullPolicy override")
			ns3 := fixture.CreateNamespace("test-1-114-env-override")
			defer func() {
				Expect(k8sClient.Delete(ctx, ns3)).To(Succeed())
			}()

			By("creating a third ArgoCD instance with explicit imagePullPolicy set to IfNotPresent (overriding operator default)")
			policy := corev1.PullIfNotPresent
			argoCD3 := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns3.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					ImagePullPolicy: &policy,
				},
			}
			Expect(k8sClient.Create(ctx, argoCD3)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, argoCD3)).To(Succeed())
			}()

			By("waiting for third ArgoCD to become available")
			Eventually(argoCD3, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying third ArgoCD deployments use explicit imagePullPolicy (IfNotPresent) instead of operator default")
			for _, deplName := range deploymentNames {
				depl := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: deplName, Namespace: ns3.Name},
				}
				Eventually(depl, "3m", "5s").Should(k8sFixture.ExistByName())

				Eventually(func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: deplName, Namespace: ns3.Name}, depl)
					if err != nil {
						return false
					}
					if len(depl.Spec.Template.Spec.Containers) == 0 {
						return false
					}
					for _, container := range depl.Spec.Template.Spec.Containers {
						if container.ImagePullPolicy != corev1.PullIfNotPresent {
							return false
						}
					}
					return true
				}, "3m", "5s").Should(BeTrue(),
					"Deployment %s in namespace %s should use explicit imagePullPolicy (IfNotPresent) overriding operator default", deplName, ns3.Name)
			}

			By("verifying third ArgoCD statefulset uses explicit imagePullPolicy (IfNotPresent)")
			ss3 := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-application-controller",
					Namespace: ns3.Name,
				},
			}
			Eventually(ss3, "3m", "5s").Should(k8sFixture.ExistByName())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "argocd-application-controller", Namespace: ns3.Name}, ss3)
				if err != nil {
					return false
				}
				if len(ss3.Spec.Template.Spec.Containers) == 0 {
					return false
				}
				for _, container := range ss3.Spec.Template.Spec.Containers {
					if container.ImagePullPolicy != corev1.PullIfNotPresent {
						return false
					}
				}
				return true
			}, "3m", "5s").Should(BeTrue(),
				"StatefulSet in namespace %s should use explicit imagePullPolicy (IfNotPresent) overriding operator default", ns3.Name)

		})

	})
})
