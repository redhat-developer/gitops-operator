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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	agentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/agent"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-125_validate_role_ownership_agent_principal", func() {

		var (
			k8sClient                client.Client
			ctx                      context.Context
			argoCD                   *argov1beta1api.ArgoCD
			ns                       *corev1.Namespace
			cleanupFunc              func()
			serviceAccount           *corev1.ServiceAccount
			role                     *rbacv1.Role
			roleBinding              *rbacv1.RoleBinding
			clusterRole              *rbacv1.ClusterRole
			clusterRoleBinding       *rbacv1.ClusterRoleBinding
			serviceNames             []string
			deploymentNames          []string
			principalDeployment      *appsv1.Deployment
			secretNames              agentFixture.AgentSecretNames
			principalNetworkPolicy   *networkingv1.NetworkPolicy
			principalRoute           *routev1.Route
			resourceProxyServiceName string
		)
		const (
			argoCDName                    = "argocd-principal"
			argoCDAgentPrincipalName      = "argocd-principal-agent-principal" // argoCDName + "-agent-principal"
			principalMetricsServiceFmt    = "%s-agent-principal-metrics"
			principalRedisProxyServiceFmt = "%s-agent-principal-redisproxy"
			principalHealthzServiceFmt    = "%s-agent-principal-healthz"
			clusterRoleName               = "argocd-principal-argocd-principal-agent-principal"
			clusterRoleBindingName        = "argocd-principal-argocd-principal-agent-principal"
			nsScopedArgoCDName            = "argocd-principal-argocd"
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ARGOCD_CLUSTER_CONFIG_NAMESPACES", "openshift-gitops, argocd-principal")

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
			ns, cleanupFunc = fixture.CreateNamespaceWithCleanupFunc("argocd-principal")

			// Define ArgoCD CR with principal enabled
			argoCD = &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      argoCDName,
					Namespace: ns.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{
					Controller: argov1beta1api.ArgoCDApplicationControllerSpec{
						Enabled: ptr.To(false),
					},
					ArgoCDAgent: &argov1beta1api.ArgoCDAgentSpec{
						Principal: &argov1beta1api.PrincipalSpec{
							Enabled:  ptr.To(true),
							Auth:     "mtls:CN=([^,]+)",
							LogLevel: "info",
							Namespace: &argov1beta1api.PrincipalNamespaceSpec{
								AllowedNamespaces: []string{
									"*",
								},
							},
							TLS: &argov1beta1api.PrincipalTLSSpec{
								InsecureGenerate: ptr.To(true),
							},
							JWT: &argov1beta1api.PrincipalJWTSpec{
								InsecureGenerate: ptr.To(true),
							},
							Server: &argov1beta1api.PrincipalServerSpec{
								KeepAliveMinInterval: "30s",
							},
						},
					},
					SourceNamespaces: []string{
						"agent-managed",
						"agent-autonomous",
					},
				},
			}

			// Define required resources for principal pod
			serviceAccount = &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      argoCDAgentPrincipalName,
					Namespace: ns.Name,
				},
			}

			role = &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      argoCDAgentPrincipalName,
					Namespace: ns.Name,
				},
			}

			roleBinding = &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      argoCDAgentPrincipalName,
					Namespace: ns.Name,
				},
			}

			clusterRole = &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterRoleName,
				},
			}

			clusterRoleBinding = &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterRoleBindingName,
				},
			}

			secretNames = agentFixture.AgentSecretNames{
				JWTSecretName:                  agentJWTSecretName,
				PrincipalTLSSecretName:         agentPrincipalTLSSecretName,
				RootCASecretName:               agentRootCASecretName,
				ResourceProxyTLSSecretName:     agentResourceProxyTLSSecretName,
				RedisInitialPasswordSecretName: "argocd-principal-redis-initial-password",
			}

			resourceProxyServiceName = fmt.Sprintf("%s-agent-principal-resource-proxy", argoCDName)
			serviceNames = []string{
				argoCDAgentPrincipalName,
				fmt.Sprintf(principalMetricsServiceFmt, argoCDName),
				fmt.Sprintf("%s-redis", argoCDName),
				fmt.Sprintf("%s-repo-server", argoCDName),
				fmt.Sprintf("%s-server", argoCDName),
				fmt.Sprintf(principalRedisProxyServiceFmt, argoCDName),
				resourceProxyServiceName,
				fmt.Sprintf(principalHealthzServiceFmt, argoCDName),
			}
			deploymentNames = []string{fmt.Sprintf("%s-redis", argoCDName), fmt.Sprintf("%s-repo-server", argoCDName), fmt.Sprintf("%s-server", argoCDName)}

			principalDeployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      argoCDAgentPrincipalName,
					Namespace: ns.Name,
				},
			}

			principalRoute = &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-agent-principal", argoCDName),
					Namespace: ns.Name,
				},
			}
			principalNetworkPolicy = &networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-agent-principal-network-policy", argoCDName),
					Namespace: ns.Name,
				},
			}

			principalNetworkPolicy = &networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-agent-principal-network-policy", argoCDName),
					Namespace: ns.Name,
				},
			}

		})

		AfterEach(func() {
			By("Cleanup cluster-scoped resources")
			if clusterRole != nil {
				_ = k8sClient.Delete(ctx, clusterRole)
			}
			if clusterRoleBinding != nil {
				_ = k8sClient.Delete(ctx, clusterRoleBinding)
			}

			By("Cleanup namespace")
			if cleanupFunc != nil {
				cleanupFunc()
			}
		})

		createRequiredSecrets := func(namespace *corev1.Namespace, additionalPrincipalSANs ...string) {
			agentFixture.CreateRequiredSecrets(agentFixture.PrincipalSecretsConfig{
				PrincipalNamespaceName:     namespace.Name,
				PrincipalServiceName:       argoCDAgentPrincipalName,
				ResourceProxyServiceName:   resourceProxyServiceName,
				JWTSecretName:              secretNames.JWTSecretName,
				PrincipalTLSSecretName:     secretNames.PrincipalTLSSecretName,
				RootCASecretName:           secretNames.RootCASecretName,
				ResourceProxyTLSSecretName: secretNames.ResourceProxyTLSSecretName,
				AdditionalPrincipalSANs:    additionalPrincipalSANs,
			})
		}

		verifyExpectedResourcesExist := func(namespace *corev1.Namespace, expectRoute ...bool) {
			var expectRoutePtr *bool
			if len(expectRoute) > 0 {
				expectRoutePtr = ptr.To(expectRoute[0])
			}

			agentFixture.VerifyExpectedResourcesExist(agentFixture.VerifyExpectedResourcesExistParams{
				Namespace:                namespace,
				ArgoCDAgentPrincipalName: argoCDAgentPrincipalName,
				ArgoCDName:               argoCDName,
				ServiceAccount:           serviceAccount,
				Role:                     role,
				RoleBinding:              roleBinding,
				ClusterRole:              clusterRole,
				ClusterRoleBinding:       clusterRoleBinding,
				PrincipalDeployment:      principalDeployment,
				PrincipalRoute:           principalRoute,
				PrincipalNetworkPolicy:   principalNetworkPolicy,
				SecretNames:              secretNames,
				ServiceNames:             serviceNames,
				DeploymentNames:          deploymentNames,
				ExpectRoute:              expectRoutePtr,
			})
		}

		It("validates that namespace-scoped resources do not delete a ClusterRole or ClusterRoleBinding with a matching generated name for Agent Principal", func() {
			By("Create ArgoCD instance")

			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("Verify expected resources are created for principal pod")

			verifyExpectedResourcesExist(ns)

			By("Create required secrets and certificates for principal pod to start properly")

			createRequiredSecrets(ns)

			By("Verify principal pod starts successfully by checking logs")

			agentFixture.VerifyLogs(argoCDAgentPrincipalName, ns.Name, []string{
				"Starting metrics server",
				"Redis proxy started",
				"Application informer synced and ready",
				"AppProject informer synced and ready",
				"Resource proxy started",
				"Namespace informer synced and ready",
				"Starting healthz server",
			})

			By("verify that deployment is in Ready state")

			Eventually(principalDeployment, "120s", "5s").Should(deploymentFixture.HaveReadyReplicas(1), "Principal deployment should become ready")

			By("Fetch Uid of clusterrole and clusterrolebinding")
			clusterRole = &rbacv1.ClusterRole{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: clusterRoleName}, clusterRole)
			}).Should(Succeed(), "ClusterRole should exist")
			initialClusterRoleUid := clusterRole.GetUID()

			clusterRoleBinding = &rbacv1.ClusterRoleBinding{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: clusterRoleBindingName}, clusterRoleBinding)
			}).Should(Succeed(), "ClusterRoleBinding should exist")
			initialClusterRoleBindingUid := clusterRoleBinding.GetUID()

			By("Create namespace-scoped ArgoCD instance namespace")

			// Create namespace for hosting namespace-scoped ArgoCD instance with principal
			nsScoped, cleanupFuncScoped := fixture.CreateNamespaceWithCleanupFunc("principal")
			defer cleanupFuncScoped()

			// Update namespace in ArgoCD CR
			argoCD.ResourceVersion = ""
			argoCD.UID = ""
			argoCD.Name = "argocd-principal-argocd"
			argoCD.Namespace = nsScoped.Name

			// Update namespace in resource references
			serviceAccount.Namespace = nsScoped.Name
			role.Namespace = nsScoped.Name
			roleBinding.Namespace = nsScoped.Name
			principalDeployment.Namespace = nsScoped.Name
			principalRoute.Namespace = nsScoped.Name
			principalNetworkPolicy.Namespace = nsScoped.Name

			By("Create namespace-scoped ArgoCD instance with principal")

			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("Verify UID of ClusterRole and ClusterRoleBinding")
			afterReconcileClusterRole := &rbacv1.ClusterRole{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: clusterRoleName}, afterReconcileClusterRole)
			}).Should(Succeed(), "ClusterRole should exist and be fetchable")
			afterReconcileClusterRoleUid := afterReconcileClusterRole.GetUID()

			afterReconcileClusterRB := &rbacv1.ClusterRoleBinding{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: clusterRoleBindingName}, afterReconcileClusterRB)
			}).Should(Succeed(), "ClusterRoleBinding should exist and be fetchable")
			afterReconcileClusterRBUid := afterReconcileClusterRB.GetUID()

			By("Verifying UID of ClusterRole and ClusterRoleBinding to ensure they are not deleted by namespaced scoped resources")
			Expect(afterReconcileClusterRoleUid).To(Equal(initialClusterRoleUid))
			Expect(afterReconcileClusterRBUid).To(Equal(initialClusterRoleBindingUid))

		})

	})
})
