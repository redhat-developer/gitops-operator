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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	appFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/argoproj/argo-cd/gitops-engine/pkg/health"
	argocdv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	"github.com/argoproj-labs/argocd-operator/common"

	agentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/agent"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {
	Context("1-054_validate_argocd_agent_destination_mapping", func() {

		const (
			destMapArgoCDPrincipalName = "argocd-hub-destmap"
			destMapArgoCDAgentName     = "argocd-spoke-destmap"

			destMapDeploymentPrincipal = "argocd-hub-destmap-agent-principal"
			destMapDeploymentAgent     = "argocd-spoke-destmap-agent-agent"

			destMapManagedClusterName = "destmap-managed-cluster"

			destMapApplicationName = "destmap-test-app"
			destMapAppProjectName  = "destmap-app-project"

			destMapNsPrincipal = "ns-hosting-destmap-principal"
			destMapNsAgent     = "ns-hosting-destmap-agent"
			destMapNsTarget    = "destmap-target-ns"

			destMapJWTSecretName              = "argocd-agent-jwt"
			destMapPrincipalTLSSecretName     = "argocd-agent-principal-tls"
			destMapRootCASecretName           = "argocd-agent-ca"
			destMapClientTLSSecretName        = "argocd-agent-client-tls"
			destMapResourceProxyTLSSecretName = "argocd-agent-resource-proxy-tls"

			destMapAgentImage = "quay.io/argoprojlabs/argocd-agent:v0.9.0"
		)

		var (
			destMapPrincipalStartupLogs = []string{
				"Starting metrics server",
				"Redis proxy started",
				"Application informer synced and ready",
				"AppProject informer synced and ready",
				"Resource proxy started",
				"Namespace informer synced and ready",
				"Starting healthz server",
			}

			destMapAgentStartupLogs = []string{
				"Starting metrics server",
				"Starting healthz server",
				"Authentication successful",
				"Connected to argocd-agent",
				"Starting event writer",
				"Starting to send events to event stream",
				"Starting to receive events from event stream",
			}
		)

		var (
			k8sClient       client.Client
			ctx             context.Context
			cleanupFuncs    []func()
			registerCleanup func(func())

			clusterRolePrincipal        *rbacv1.ClusterRole
			clusterRoleBindingPrincipal *rbacv1.ClusterRoleBinding
			clusterRoleAgent            *rbacv1.ClusterRole
			clusterRoleBindingAgent     *rbacv1.ClusterRoleBinding
			adminCRBAgent               *rbacv1.ClusterRoleBinding
		)

		// buildDestMapPrincipalCR builds the principal ArgoCD CR with destination-based mapping enabled.
		buildDestMapPrincipalCR := func(ns *corev1.Namespace) *argov1beta1api.ArgoCD {
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      destMapArgoCDPrincipalName,
					Namespace: ns.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{
					Controller: argov1beta1api.ArgoCDApplicationControllerSpec{
						Enabled: ptr.To(false),
					},
					ArgoCDAgent: &argov1beta1api.ArgoCDAgentSpec{
						Principal: &argov1beta1api.PrincipalSpec{
							Enabled:                 ptr.To(true),
							DestinationBasedMapping: ptr.To(true),
							Auth:                    "mtls:CN=([^,]+)",
							LogLevel:                "debug",
							Image:                   destMapAgentImage,
							Namespace: &argov1beta1api.PrincipalNamespaceSpec{
								AllowedNamespaces: []string{
									destMapNsPrincipal,
								},
							},
							TLS: &argov1beta1api.PrincipalTLSSpec{
								InsecureGenerate: ptr.To(false),
							},
							JWT: &argov1beta1api.PrincipalJWTSpec{
								InsecureGenerate: ptr.To(false),
							},
							Server: &argov1beta1api.PrincipalServerSpec{
								KeepAliveMinInterval: "30s",
								Route: argov1beta1api.ArgoCDAgentPrincipalRouteSpec{
									Enabled: ptr.To(false),
								},
								Service: argov1beta1api.ArgoCDAgentPrincipalServiceSpec{
									Type: corev1.ServiceTypeLoadBalancer,
								},
							},
						},
						Agent: &argov1beta1api.AgentSpec{
							Enabled: ptr.To(false),
						},
					},
				},
			}
			return argoCD
		}

		// buildDestMapAgentCR builds the managed agent ArgoCD CR with destination-based mapping enabled.
		buildDestMapAgentCR := func(ns *corev1.Namespace) *argov1beta1api.ArgoCD {
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      destMapArgoCDAgentName,
					Namespace: ns.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{
					Server: argov1beta1api.ArgoCDServerSpec{
						Enabled: ptr.To(false),
					},
					SourceNamespaces: []string{
						destMapNsAgent,
					},
					ArgoCDAgent: &argov1beta1api.ArgoCDAgentSpec{
						Principal: &argov1beta1api.PrincipalSpec{
							Enabled: ptr.To(false),
						},
						Agent: &argov1beta1api.AgentSpec{
							Enabled:  ptr.To(true),
							Creds:    "mtls:any",
							LogLevel: "debug",
							Image:    destMapAgentImage,
							Client: &argov1beta1api.AgentClientSpec{
								PrincipalServerAddress: "",
								PrincipalServerPort:    "443",
								KeepAliveInterval:      "50s",
								Mode:                   string(argov1beta1api.AgentModeManaged),
							},
							Redis: &argov1beta1api.AgentRedisSpec{
								ServerAddress: fmt.Sprintf("%s-%s:%d", destMapArgoCDAgentName, "redis", common.ArgoCDDefaultRedisPort),
							},
							TLS: &argov1beta1api.AgentTLSSpec{
								SecretName:       destMapClientTLSSecretName,
								RootCASecretName: destMapRootCASecretName,
								Insecure:         ptr.To(false),
							},
							DestinationBasedMapping: &argov1beta1api.DestinationBasedMappingSpec{
								Enabled:         ptr.To(true),
								CreateNamespace: ptr.To(true),
							},
							AllowedNamespaces: []string{
								destMapNsAgent,
							},
						},
					},
				},
			}
			return argoCD
		}

		// buildDestMapAppProject builds the AppProject for destination-based mapping tests.
		buildDestMapAppProject := func(nsName string) *argocdv1alpha1.AppProject {
			return &argocdv1alpha1.AppProject{
				ObjectMeta: metav1.ObjectMeta{
					Name:      destMapAppProjectName,
					Namespace: nsName,
				},
				Spec: argocdv1alpha1.AppProjectSpec{
					ClusterResourceWhitelist: []argocdv1alpha1.ClusterResourceRestrictionItem{{
						Group: "*",
						Kind:  "*",
					}},
					SourceRepos:      []string{"*"},
					SourceNamespaces: []string{"*"},
					Destinations: []argocdv1alpha1.ApplicationDestination{{
						Name:      destMapManagedClusterName,
						Namespace: destMapNsTarget,
						Server:    "*",
					}},
				},
			}
		}

		// buildDestMapApplication builds the Application CR with destination.name routing.
		buildDestMapApplication := func() *argocdv1alpha1.Application {
			return &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      destMapApplicationName,
					Namespace: destMapNsPrincipal,
				},
				Spec: argocdv1alpha1.ApplicationSpec{
					Project: destMapAppProjectName,
					Source: &argocdv1alpha1.ApplicationSource{
						RepoURL:        "https://github.com/redhat-developer/openshift-gitops-getting-started",
						TargetRevision: "HEAD",
						Path:           "app",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Name:      destMapManagedClusterName,
						Namespace: destMapNsTarget,
					},
					SyncPolicy: &argocdv1alpha1.SyncPolicy{
						Automated: &argocdv1alpha1.SyncPolicyAutomated{
							Prune:    ptr.To(true),
							SelfHeal: ptr.To(true),
						},
						ManagedNamespaceMetadata: &argocdv1alpha1.ManagedNamespaceMetadata{
							Labels: map[string]string{
								"argocd.argoproj.io/managed-by": destMapArgoCDAgentName,
							},
						},
					},
				},
			}
		}

		// deployDestMapPrincipal deploys the principal ArgoCD instance with destination-based mapping,
		// and returns the created ArgoCD CR.
		deployDestMapPrincipal := func() *argov1beta1api.ArgoCD {
			GinkgoHelper()

			nsPrincipal, cleanup := fixture.CreateNamespaceWithCleanupFunc(destMapNsPrincipal)
			registerCleanup(cleanup)

			By("Create ArgoCD instance with principal component and destination-based mapping enabled")
			argoCDInstance := buildDestMapPrincipalCR(nsPrincipal)
			if !fixture.RunningOnOpenShift() {
				argoCDInstance.Spec.ArgoCDAgent.Principal.Server.Service.Type = corev1.ServiceTypeClusterIP
			}
			Expect(k8sClient.Create(ctx, argoCDInstance)).To(Succeed())

			By("Wait for principal service to be ready")
			additionalSANs := []string{}
			if fixture.RunningOnOpenShift() {
				principalService := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      destMapDeploymentPrincipal,
						Namespace: nsPrincipal.Name,
					},
				}
				err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
					if pollErr := k8sClient.Get(ctx, client.ObjectKeyFromObject(principalService), principalService); pollErr != nil {
						return false, nil
					}
					for _, ingress := range principalService.Status.LoadBalancer.Ingress {
						switch {
						case ingress.Hostname != "":
							additionalSANs = append(additionalSANs, ingress.Hostname)
							return true, nil
						case ingress.IP != "":
							additionalSANs = append(additionalSANs, ingress.IP)
							return true, nil
						}
					}
					return false, nil
				})
				if err != nil {
					GinkgoWriter.Println("LoadBalancer ingress not available, proceeding without external SANs:", err)
				}
			} else {
				GinkgoWriter.Println("Cluster does not support LoadBalancer services; using in-cluster service DNS SANs only")
			}

			By("Create required secrets for principal")
			agentFixture.CreateRequiredSecrets(agentFixture.PrincipalSecretsConfig{
				PrincipalNamespaceName:     destMapNsPrincipal,
				PrincipalServiceName:       destMapDeploymentPrincipal,
				ResourceProxyServiceName:   fmt.Sprintf("%s-agent-principal-resource-proxy", destMapArgoCDPrincipalName),
				JWTSecretName:              destMapJWTSecretName,
				PrincipalTLSSecretName:     destMapPrincipalTLSSecretName,
				RootCASecretName:           destMapRootCASecretName,
				ResourceProxyTLSSecretName: destMapResourceProxyTLSSecretName,
				AdditionalPrincipalSANs:    additionalSANs,
			})

			By("Verify that principal deployment is in Ready state")
			Eventually(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
				Name:      destMapDeploymentPrincipal,
				Namespace: nsPrincipal.Name}}, "240s", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			By("Verify principal logs contain expected messages")
			agentFixture.VerifyLogs(destMapDeploymentPrincipal, nsPrincipal.Name, destMapPrincipalStartupLogs)

			return argoCDInstance
		}

		// deployDestMapAgent deploys the managed agent ArgoCD instance with destination-based mapping,
		// and returns the created ArgoCD CR.
		deployDestMapAgent := func() *argov1beta1api.ArgoCD {
			GinkgoHelper()

			nsAgent, cleanup := fixture.CreateNamespaceWithCleanupFunc(destMapNsAgent)
			registerCleanup(cleanup)

			By("Create required secrets for managed agent")
			agentFixture.CreateRequiredAgentSecrets(agentFixture.AgentSecretsConfig{
				AgentNamespace:            nsAgent,
				PrincipalNamespaceName:    destMapNsPrincipal,
				PrincipalRootCASecretName: destMapRootCASecretName,
				AgentRootCASecretName:     destMapRootCASecretName,
				ClientTLSSecretName:       destMapClientTLSSecretName,
				ClientCommonName:          destMapManagedClusterName,
			})

			By("Create cluster registration secret for managed agent")
			agentFixture.CreateClusterRegistrationSecret(agentFixture.ClusterRegistrationSecretConfig{
				PrincipalNamespaceName:    destMapNsPrincipal,
				AgentNamespaceName:        nsAgent.Name,
				AgentName:                 destMapManagedClusterName,
				ResourceProxyServiceName:  fmt.Sprintf("%s-agent-principal-resource-proxy", destMapArgoCDPrincipalName),
				ResourceProxyPort:         9090,
				PrincipalRootCASecretName: destMapRootCASecretName,
				AgentTLSSecretName:        destMapClientTLSSecretName,
			})

			By("Deploy managed agent ArgoCD instance with destination-based mapping")
			argoCDInstanceAgent := buildDestMapAgentCR(nsAgent)
			argoCDInstanceAgent.Spec.ArgoCDAgent.Agent.Client.PrincipalServerAddress = fmt.Sprintf("%s.%s.svc", destMapDeploymentPrincipal, destMapNsPrincipal)
			Expect(k8sClient.Create(ctx, argoCDInstanceAgent)).To(Succeed())

			By("Verify managed agent deployment is in Ready state")
			Eventually(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
				Name:      destMapDeploymentAgent,
				Namespace: nsAgent.Name}}, "120s", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			By("Verify managed agent logs contain expected messages")
			agentFixture.VerifyLogs(destMapDeploymentAgent, nsAgent.Name, destMapAgentStartupLogs)

			return argoCDInstanceAgent
		}

		createAdminCRBForAgent := func() {
			adminCRBAgent = &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-admin-crb", destMapNsAgent),
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: rbacv1.GroupName,
					Kind:     "ClusterRole",
					Name:     "admin",
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      rbacv1.ServiceAccountKind,
						Name:      fmt.Sprintf("%s-argocd-application-controller", destMapArgoCDAgentName),
						Namespace: destMapNsAgent,
					},
				},
			}

			existingCRB := &rbacv1.ClusterRoleBinding{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(adminCRBAgent), existingCRB); err == nil {
				Expect(k8sClient.Delete(ctx, existingCRB)).To(Succeed())
			}
			Expect(k8sClient.Create(ctx, adminCRBAgent)).To(Succeed())
		}

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ARGOCD_CLUSTER_CONFIG_NAMESPACES",
				"openshift-gitops, "+destMapNsPrincipal+", "+destMapNsAgent)

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
			cleanupFuncs = nil
			registerCleanup = func(fn func()) {
				if fn != nil {
					cleanupFuncs = append(cleanupFuncs, fn)
				}
			}

			createAdminCRBForAgent()

			clusterRolePrincipal = &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-%s-agent-principal", destMapArgoCDPrincipalName, destMapNsPrincipal),
				},
			}
			clusterRoleBindingPrincipal = &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-%s-agent-principal", destMapArgoCDPrincipalName, destMapNsPrincipal),
				},
			}
			clusterRoleAgent = &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-%s-agent-agent", destMapArgoCDAgentName, destMapNsAgent),
				},
			}
			clusterRoleBindingAgent = &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-%s-agent-agent", destMapArgoCDAgentName, destMapNsAgent),
				},
			}

			_, cleanupFuncTarget := fixture.CreateClusterScopedManagedNamespaceWithCleanupFunc(destMapNsTarget, destMapArgoCDAgentName)
			registerCleanup(cleanupFuncTarget)
		})

		// This test verifies:
		// 1. Principal and agent with destination-based mapping enabled can be deployed and connected.
		// 2. An Application created in the principal's namespace is routed to the agent via destination.name.
		// 3. The Application reaches Synced and Healthy status.
		// TODO: update the test to pass on xKS cluster
		It("Should deploy principal and agent with destination-based mapping", Label("openshift"), func() {

			By("Deploy principal with destination-based mapping enabled")
			principalArgoCD := deployDestMapPrincipal()

			By("Deploy managed agent with destination-based mapping enabled")
			agentArgoCD := deployDestMapAgent()

			By("Verify principal and agent ArgoCD instances are available")
			Eventually(principalArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())
			Eventually(agentArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("Verify principal is connected to the managed agent")
			agentFixture.VerifyLogs(destMapDeploymentPrincipal, destMapNsPrincipal, []string{
				fmt.Sprintf("Mapped cluster %s to agent %s", destMapManagedClusterName, destMapManagedClusterName),
				fmt.Sprintf("Updated connection status to 'Successful' in Cluster: '%s' mapped with Agent: '%s'", destMapManagedClusterName, destMapManagedClusterName),
			})

			By("Create AppProject for destination-based mapping in principal namespace")
			Expect(k8sClient.Create(ctx, buildDestMapAppProject(destMapNsPrincipal))).To(Succeed())

			By("Verify if the AppProject is synced to the agent")
			Eventually(buildDestMapAppProject(destMapNsAgent), "120s", "5s").Should(k8sFixture.ExistByNameWithClient(k8sClient))

			application := buildDestMapApplication()

			By("Deploy application in principal namespace: " + destMapNsPrincipal)
			Expect(k8sClient.Create(ctx, application)).To(Succeed())

			By("Verify application is routed to agent - check if it appears in agent namespace")
			agentApp := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      destMapApplicationName,
					Namespace: destMapNsAgent,
				},
			}
			Eventually(agentApp, "180s", "5s").Should(k8sFixture.ExistByNameWithClient(k8sClient),
				"Application should be routed from principal to agent namespace")

			By("Verify application on agent is synced and healthy")
			Eventually(agentApp, "4m", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced),
				"Application on agent should be synced")
			Eventually(agentApp, "4m", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy),
				"Application on agent should be healthy")

			By("Verify application is synced and healthy on the principal")
			Eventually(application, "180s", "5s").Should(appFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced),
				"Application should be synced on principal")
			Eventually(application, "180s", "5s").Should(appFixture.HaveHealthStatusCode(health.HealthStatusHealthy),
				"Application should be healthy on principal")
		})

		AfterEach(func() {
			fixture.OutputDebugOnFail(destMapNsPrincipal, destMapNsAgent, destMapNsTarget)

			By("Cleanup cluster-scoped resources")
			_ = k8sClient.Delete(ctx, clusterRolePrincipal)
			_ = k8sClient.Delete(ctx, clusterRoleBindingPrincipal)
			_ = k8sClient.Delete(ctx, clusterRoleAgent)
			_ = k8sClient.Delete(ctx, clusterRoleBindingAgent)
			_ = k8sClient.Delete(ctx, adminCRBAgent)

			By("Cleanup namespaces created in this test")
			for i := len(cleanupFuncs) - 1; i >= 0; i-- {
				cleanupFuncs[i]()
			}
		})
	})
})
