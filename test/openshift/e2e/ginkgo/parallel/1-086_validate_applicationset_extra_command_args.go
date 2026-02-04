package parallel

import (
	"context"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deplFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-086_validate_applicationset_extra_command_args", func() {

		var (
			k8sClient   client.Client
			ctx         context.Context
			ns          *corev1.Namespace
			cleanupFunc func()
			argoCD      *argov1beta1api.ArgoCD
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		AfterEach(func() {
			defer cleanupFunc()
			fixture.OutputDebugOnFail(ns)
		})

		It("validates that extra command arguments are added to the ApplicationSet controller deployment", func() {

			expectAppSetIsReady := func() {
				Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

				appSetDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-applicationset-controller", Namespace: ns.Name}}

				Eventually(appSetDepl).Should(k8sFixture.ExistByName())
				//check for ReadyReplicas to make sure the pod is actually running.
				Eventually(appSetDepl).Should(deplFixture.HaveReadyReplicas(1))
			}

			By("creating a simple namespace-scoped Argo CD instance with ApplicationSet enabled")
			ns, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			argoCD = &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					ApplicationSet: &argov1beta1api.ArgoCDApplicationSet{},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for the initial ApplicationSet controller instance to be ready")
			expectAppSetIsReady()

			By("patching the ArgoCD CR to add an extra command argument")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ApplicationSet.ExtraCommandArgs = []string{"--debug"}
			})

			By("waiting for the ApplicationSet controller to reconcile and adopt the new arguments")
			expectAppSetIsReady()

			By("verifying the new command arguments are present in the Deployment spec")
			appSetDepl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-applicationset-controller", Namespace: ns.Name}}

			expectedArg := "--debug"

			Eventually(appSetDepl).Should(deplFixture.HaveContainerCommandSubstring(expectedArg, 0),
				"Expected the applicationset-controller command to include the extra argument: %s", expectedArg)
		})
	})
})
