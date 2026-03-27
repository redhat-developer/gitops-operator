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

package sequential

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	agentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/agent"
	appFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdClient "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocdclient"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	routeFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/route"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"

	argocdv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"

	routev1 "github.com/openshift/api/route/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// This test validates that terminal streaming works through the ArgoCD agent architecture
// on OpenShift. It exercises the full terminal flow:
var _ = Describe("GitOps Operator Sequential E2E Tests", func() {
	Context("1-054_validate_argocd_agent_terminal_streaming", func() {
		var (
			k8sClient       client.Client
			ctx             context.Context
			cleanupFuncs    []func()
			registerCleanup func(func())

			clusterRolePrincipal           *rbacv1.ClusterRole
			clusterRoleBindingPrincipal    *rbacv1.ClusterRoleBinding
			clusterRoleManagedAgent        *rbacv1.ClusterRole
			clusterRoleBindingManagedAgent *rbacv1.ClusterRoleBinding
			adminCRBManagedAgent           *rbacv1.ClusterRoleBinding
			adminCRBAgentAgent             *rbacv1.ClusterRoleBinding
		)

		BeforeEach(func() {
			if !fixture.EnvLocalRun() {
				fixture.EnsureSequentialCleanSlate()
				fixture.SetEnvInOperatorSubscriptionOrDeployment("ARGOCD_CLUSTER_CONFIG_NAMESPACES",
					"openshift-gitops, "+namespaceAgentPrincipal+", "+namespaceManagedAgent)
			}

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
			cleanupFuncs = nil
			registerCleanup = func(fn func()) {
				if fn != nil {
					cleanupFuncs = append(cleanupFuncs, fn)
				}
			}

			// create required cluster roles and cluster role bindings for the test
			adminCRBManagedAgent = &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-admin-crb", namespaceManagedAgent),
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: rbacv1.GroupName,
					Kind:     "ClusterRole",
					Name:     "admin",
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      rbacv1.ServiceAccountKind,
						Name:      fmt.Sprintf("%s-argocd-application-controller", argoCDAgentInstanceNameAgent),
						Namespace: namespaceManagedAgent,
					},
				},
			}
			Expect(k8sClient.Create(ctx, adminCRBManagedAgent)).To(Succeed())

			adminCRBAgentAgent = &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-agent-agent-admin-crb", namespaceManagedAgent),
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: rbacv1.GroupName,
					Kind:     "ClusterRole",
					Name:     "admin",
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      rbacv1.ServiceAccountKind,
						Name:      fmt.Sprintf("%s-agent-agent", argoCDAgentInstanceNameAgent),
						Namespace: namespaceManagedAgent,
					},
				},
			}
			Expect(k8sClient.Create(ctx, adminCRBAgentAgent)).To(Succeed())

			clusterRolePrincipal = &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-%s-agent-principal", argoCDAgentInstanceNamePrincipal, namespaceAgentPrincipal),
				},
			}

			clusterRoleBindingPrincipal = &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-%s-agent-principal", argoCDAgentInstanceNamePrincipal, namespaceAgentPrincipal),
				},
			}

			clusterRoleManagedAgent = &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-%s-agent-agent", argoCDAgentInstanceNameAgent, namespaceManagedAgent),
				},
			}
			clusterRoleBindingManagedAgent = &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-%s-agent-agent", argoCDAgentInstanceNameAgent, namespaceManagedAgent),
				},
			}

			// create required namespaces for the test
			_, cleanupFuncClusterManaged := fixture.CreateNamespaceWithCleanupFunc(managedAgentClusterName)
			registerCleanup(cleanupFuncClusterManaged)

			_, cleanupFuncClusterAutonomous := fixture.CreateNamespaceWithCleanupFunc(autonomousAgentClusterName)
			registerCleanup(cleanupFuncClusterAutonomous)

			_, cleanupFuncManagedApplication := fixture.CreateClusterScopedManagedNamespaceWithCleanupFunc(
				managedAgentApplicationNamespace, argoCDAgentInstanceNameAgent)
			registerCleanup(cleanupFuncManagedApplication)
		})

		It("Should open a terminal session to a pod deployed via ArgoCD agent and execute commands", func() {

			By("Deploy principal with server route enabled and verify it starts successfully")
			deployPrincipal(ctx, k8sClient, registerCleanup, true)

			By("Enable exec feature in ArgoCD server configuration")
			enableExecInArgoCD(ctx, k8sClient, argoCDAgentInstanceNamePrincipal, namespaceAgentPrincipal)

			By("Wait for ArgoCD server to restart with exec enabled")
			Eventually(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-server", argoCDAgentInstanceNamePrincipal),
				Namespace: namespaceAgentPrincipal,
			}}, "120s", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			By("Deploy managed agent and verify it starts successfully")
			deployAgent(ctx, k8sClient, registerCleanup, argov1beta1api.AgentModeManaged)

			By("Wait for agent repo-server to be ready before creating applications")
			Eventually(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-repo-server", argoCDAgentInstanceNameAgent),
				Namespace: namespaceManagedAgent,
			}}, "180s", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			By("Verify principal is connected to managed agent")
			agentFixture.VerifyLogs(deploymentNameAgentPrincipal, namespaceAgentPrincipal, []string{
				fmt.Sprintf("Mapped cluster %s to agent %s", managedAgentClusterName, managedAgentClusterName),
				fmt.Sprintf("Updated connection status to 'Successful' in Cluster: '%s' mapped with Agent: '%s'",
					managedAgentClusterName, managedAgentClusterName),
			})

			By("Create AppProject for managed agent in " + namespaceAgentPrincipal)
			Expect(k8sClient.Create(ctx, buildAppProjectResource(namespaceAgentPrincipal, argov1beta1api.AgentModeManaged))).To(Succeed())

			application := buildApplicationResource("terminal-app",
				managedAgentClusterName, managedAgentClusterName, argoCDAgentInstanceNameAgent, argov1beta1api.AgentModeManaged)

			By("Deploy application for terminal testing")
			Expect(k8sClient.Create(ctx, application)).To(Succeed())

			By("Verify application is synced and healthy")
			Eventually(application, "180s", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced),
				"Application should be synced")
			Eventually(application, "180s", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy),
				"Application should be healthy")

			By("Wait for ArgoCD server Route to be created")
			serverRoute := &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-server", argoCDAgentInstanceNamePrincipal),
					Namespace: namespaceAgentPrincipal,
				},
			}
			Eventually(serverRoute, "120s", "5s").Should(k8sFixture.ExistByName())
			Eventually(serverRoute, "120s", "5s").Should(routeFixture.HaveAdmittedIngress())

			// Create ArgoCD client using the ArgoCD server Route and admin password.
			// ArgoCD Client is acting as a browser and trying to open a terminal session
			// to the application in the managed-cluster.
			By("Get ArgoCD admin password and login via Route")
			argoEndpoint := serverRoute.Spec.Host
			GinkgoWriter.Printf("ArgoCD server Route host: %s\n", argoEndpoint)

			password := agentFixture.GetInitialAdminSecretPassword(argoCDAgentInstanceNamePrincipal, namespaceAgentPrincipal, k8sClient)
			argoClient := argocdClient.NewArgoClient(argoEndpoint, "admin", password)
			Expect(argoClient.Login()).To(Succeed())

			// Find the application pod which we want to open a terminal session for.
			By("Find a running pod in the application namespace")
			var podName, containerName string
			Eventually(func() bool {
				pods := &corev1.PodList{}
				if err := k8sClient.List(ctx, pods, client.InNamespace(managedAgentApplicationNamespace)); err != nil {
					GinkgoWriter.Println("Failed to list pods:", err)
					return false
				}
				for _, p := range pods.Items {
					if strings.Contains(p.Name, "spring-petclinic") &&
						p.Status.Phase == corev1.PodRunning && len(p.Spec.Containers) > 0 {
						podName = p.Name
						containerName = p.Spec.Containers[0].Name
						return true
					}
				}
				return false
			}, "60s", "5s").Should(BeTrue(), "expected a running spring-petclinic pod in %s", managedAgentApplicationNamespace)

			// Open a terminal session with ArgoCD Server API.
			// This replicates the behavior of the ArgoCD UI when a user opens a terminal
			// session to an application. This is done by sending a resize message to the
			// shell and then sending commands to the shell. The shell will execute the
			// command and stream the output back to the principal. The principal will then
			// stream the output back to the UI.
			GinkgoWriter.Printf("Opening terminal session to pod %s, container %s\n", podName, containerName)

			// We use WebSoket for Test to ArgoCD Server communication.
			// Then internally agent will first try Web-socket to pods/exec
			// and if that fails, it will fallback to SPDY.
			By("Open terminal session via ArgoCD WebSocket API")
			var session *argocdClient.TerminalClient
			Eventually(func() error {
				var err error
				session, err = argoClient.ExecTerminal(application, managedAgentApplicationNamespace, podName, containerName)
				return err
			}, "30s", "5s").Should(Succeed(), "failed to open terminal session")
			defer session.Close()

			// Send a resize message first, this is required by the shell to render the
			// output content accordingly.
			err := session.SendResize(80, 24)
			Expect(err).ToNot(HaveOccurred(), "failed to send resize")

			// Wait for shell to initialize by checking for any output
			Eventually(func() bool {
				return len(session.GetOutput()) > 0
			}, 10*time.Second, 1*time.Second).Should(BeTrue(), "shell did not initialize")

			// Test 1: Run 'pwd' command
			err = session.SendInput("pwd; echo PWD_DONE\n")
			Expect(err).ToNot(HaveOccurred(), "failed to send pwd command")
			found := session.WaitForOutput("PWD_DONE", 10*time.Second)
			Expect(found).To(BeTrue(), "expected to find 'PWD_DONE' in pwd output, got: %s", session.GetOutput())
			GinkgoWriter.Println("Test 1 passed: pwd command executed successfully")

			// Test 2: Run 'whoami' command
			err = session.SendInput("whoami; echo WHOAMI_DONE\n")
			Expect(err).ToNot(HaveOccurred(), "failed to send whoami command")
			found = session.WaitForOutput("whoami", 10*time.Second)
			Expect(found).To(BeTrue(), "expected whoami output in terminal, got: %s", session.GetOutput())
			GinkgoWriter.Println("Test 2 passed: whoami command executed successfully")

			// Test 3: Run 'ls' command - should list files
			err = session.SendInput("ls; echo LS_DONE\n")
			Expect(err).ToNot(HaveOccurred(), "failed to send ls command")
			found = session.WaitForOutput("LS_DONE", 10*time.Second)
			Expect(found).To(BeTrue(), "expected to find 'LS_DONE' in ls output, got: %s", session.GetOutput())
			GinkgoWriter.Println("Test 3 passed: ls command executed successfully")

			GinkgoWriter.Println("All terminal commands executed successfully.")
		})

		AfterEach(func() {
			fixture.OutputDebugOnFail(namespaceAgentPrincipal, namespaceManagedAgent, managedAgentClusterName, managedAgentApplicationNamespace)

			By("Cleanup cluster-scoped resources")
			_ = k8sClient.Delete(ctx, clusterRolePrincipal)
			_ = k8sClient.Delete(ctx, clusterRoleBindingPrincipal)
			_ = k8sClient.Delete(ctx, clusterRoleManagedAgent)
			_ = k8sClient.Delete(ctx, clusterRoleBindingManagedAgent)
			_ = k8sClient.Delete(ctx, adminCRBManagedAgent)
			_ = k8sClient.Delete(ctx, adminCRBAgentAgent)

			By("Cleanup namespaces created in this test")
			for i := len(cleanupFuncs) - 1; i >= 0; i-- {
				cleanupFuncs[i]()
			}
		})
	})
})

// enableExecInArgoCD configures the ArgoCD CR to enable the web-based terminal.
// through spec.extraConfig and grant the admin role exec permission via spec.rbac.policy.
func enableExecInArgoCD(ctx context.Context, k8sClient client.Client, argocdName, namespace string) {
	GinkgoHelper()

	argoCD := &argov1beta1api.ArgoCD{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{
		Name:      argocdName,
		Namespace: namespace,
	}, argoCD)).To(Succeed())

	if argoCD.Spec.ExtraConfig == nil {
		argoCD.Spec.ExtraConfig = map[string]string{}
	}
	argoCD.Spec.ExtraConfig["exec.enabled"] = "true"
	argoCD.Spec.ExtraConfig["exec.shells"] = "bash,sh,ash,/bin/bash,/bin/sh,/bin/ash"

	execPolicy := "p, role:admin, exec, create, */*, allow"
	argoCD.Spec.RBAC.Policy = &execPolicy

	Expect(k8sClient.Update(ctx, argoCD)).To(Succeed())
}
