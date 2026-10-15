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

GITOPS-9332: Add ginkgo test to verify creating an application in core mode
Docs: https://docs.redhat.com/en/documentation/red_hat_openshift_gitops/1.21/html/argo_cd_applications/creating-an-application-using-gitops-argocd-cli
*/

package parallel

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	argov1beta1api "github.com/argoproj-labs/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/argoproj/argo-cd/gitops-engine/pkg/health"
	argocdv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	appFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const argoCDCoreModeCLITimeout = 2 * time.Minute

// runArgoCDCoreMode runs an argocd CLI command with --core using a dedicated kubeconfig
// whose current context namespace is set to the Argo CD namespace. This keeps parallel tests from
// mutating the shared kubeconfig, and avoids requiring an Argo CD API login.
// Each invocation is bounded by argoCDCoreModeCLITimeout so a hung CLI cannot block the suite.
func runArgoCDCoreMode(kubeconfigPath, repoServerName string, args ...string) (string, error) {
	cmdArgs := append([]string{"argocd"}, args...)
	cmdArgs = append(cmdArgs, "--core", "--repo-server-name", repoServerName)

	GinkgoWriter.Println("executing command:", cmdArgs, "KUBECONFIG="+kubeconfigPath)

	ctx, cancel := context.WithTimeout(context.Background(), argoCDCoreModeCLITimeout)
	defer cancel()

	// #nosec G204
	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfigPath)

	outputBytes, err := cmd.CombinedOutput()
	output := string(outputBytes)
	GinkgoWriter.Println(output)
	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("argocd command timed out after %s: %w\noutput: %s", argoCDCoreModeCLITimeout, ctx.Err(), output)
	}
	return output, err
}

// createCoreModeKubeconfig loads and flattens the active kubeconfig (including multi-path
// KUBECONFIG values and relative cert/key paths), writes it to a temp file, and sets the
// current context namespace to argoCDNamespace so core-mode CLI calls target that instance.
func createCoreModeKubeconfig(argoCDNamespace string) (string, func()) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	cfg, err := loadingRules.Load()
	Expect(err).ToNot(HaveOccurred())

	Expect(clientcmdapi.FlattenConfig(cfg)).To(Succeed())

	currentContext := cfg.CurrentContext
	Expect(currentContext).ToNot(BeEmpty(), "kubeconfig has no current context")
	contextCfg, ok := cfg.Contexts[currentContext]
	Expect(ok).To(BeTrue(), "current context %q not found in kubeconfig", currentContext)
	Expect(contextCfg).ToNot(BeNil())
	contextCfg.Namespace = argoCDNamespace

	tmpFile, err := os.CreateTemp("", "argocd-core-mode-kubeconfig-*")
	Expect(err).ToNot(HaveOccurred())
	tmpPath := tmpFile.Name()
	Expect(tmpFile.Close()).To(Succeed())

	Expect(clientcmd.WriteToFile(*cfg, tmpPath)).To(Succeed())

	cleanup := func() {
		_ = os.Remove(tmpPath)
	}
	return tmpPath, cleanup
}

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-144_validate_argocd_cli_core_mode", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies creating an application via argocd CLI in core mode", func() {

			By("creating a namespace-scoped Argo CD instance")
			ns, cleanupNS := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupNS()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("creating a destination namespace managed by the Argo CD instance")
			destNS, cleanupDestNS := fixture.CreateManagedNamespaceWithCleanupFunc(ns.Name+"-apps", ns.Name)
			defer cleanupDestNS()

			By("waiting for application-controller RBAC in the managed destination namespace")
			Eventually(&rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-application-controller", Namespace: destNS.Name},
			}).Should(k8sFixture.ExistByName())

			By("creating a temporary kubeconfig with the Argo CD namespace as the current context namespace")
			kubeconfigPath, cleanupKubeconfig := createCoreModeKubeconfig(ns.Name)
			defer cleanupKubeconfig()

			repoServerName := argoCD.Name + "-repo-server"
			appName := "test-1-144-nginx"

			By("verifying argocd CLI works in core mode by listing applications")
			_, err := runArgoCDCoreMode(kubeconfigPath, repoServerName, "app", "list")
			Expect(err).ToNot(HaveOccurred())

			By("creating an application in core mode via the argocd CLI")
			_, err = runArgoCDCoreMode(kubeconfigPath, repoServerName,
				"app", "create", appName,
				"--repo", "https://github.com/redhat-developer/gitops-operator",
				"--path", "test/examples/nginx",
				"--revision", "HEAD",
				"--dest-server", "https://kubernetes.default.svc",
				"--dest-namespace", destNS.Name,
				"--sync-policy", "automated",
				"--self-heal",
				"--sync-option", "Prune=true",
			)
			Expect(err).ToNot(HaveOccurred())

			app := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: ns.Name},
			}

			By("verifying the Application CR was created by the CLI")
			Eventually(app).Should(k8sFixture.ExistByName())

			By("verifying the application becomes Healthy and Synced")
			Eventually(app, "4m", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy))
			Eventually(app, "4m", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced))

			By("verifying argocd app list --core shows the application")
			Eventually(func() (string, error) {
				return runArgoCDCoreMode(kubeconfigPath, repoServerName, "app", "list")
			}, "2m", "5s").Should(And(
				ContainSubstring(appName),
				ContainSubstring("Healthy"),
				ContainSubstring("Synced"),
			))
		})
	})
})
