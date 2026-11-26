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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argov1beta1api "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argocdagent"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	agentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/agent"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {
	const (
		argoCDName               = "example"
		argoCDAgentPrincipalName = "example-agent-principal"
		haTestNamespace          = "argocd-agent-principal-ha-1-137"
	)

	Context("1-137_validate_argocd_agent_principal_ha", func() {
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
			principalNetworkPolicy   *networkingv1.NetworkPolicy
			secretNames              agentFixture.AgentSecretNames
			resourceProxyServiceName string
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ARGOCD_CLUSTER_CONFIG_NAMESPACES", "openshift-gitops, "+haTestNamespace)

			ctx = context.Background()
			ns, cleanupFunc = fixture.CreateNamespaceWithCleanupFunc(haTestNamespace)

			adminPort := int32(argocdagent.PrincipalHAAdminPort)
			argoCD = &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      argoCDName,
					Namespace: ns.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{
					Controller: argov1beta1api.ArgoCDApplicationControllerSpec{
						Enabled: new(false),
					},
					ArgoCDAgent: &argov1beta1api.ArgoCDAgentSpec{
						Principal: &argov1beta1api.PrincipalSpec{
							Enabled:  new(true),
							Auth:     "mtls:CN=([^,]+)",
							LogLevel: "info",
							Namespace: &argov1beta1api.PrincipalNamespaceSpec{
								AllowedNamespaces: []string{"*"},
							},
							TLS: &argov1beta1api.PrincipalTLSSpec{
								InsecureGenerate: new(true),
							},
							JWT: &argov1beta1api.PrincipalJWTSpec{
								InsecureGenerate: new(true),
							},
							Server: &argov1beta1api.PrincipalServerSpec{
								KeepAliveMinInterval: "30s",
							},
							HA: &argov1beta1api.PrincipalHASpec{
								Enabled:                      new(true),
								PreferredRole:                "primary",
								PeerAddress:                  "peer.example.com:443",
								AllowedReplicationClients:    []string{"argocd-agent-principal-peer"},
								AdminPort:                    &adminPort,
								AdminAuth:                    "mtls:subject:CN=ha-admin",
								ReplicationInitialAckTimeout: "10m",
							},
						},
					},
					SourceNamespaces: []string{
						"agent-managed",
						"agent-autonomous",
					},
				},
			}

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
					Name: fmt.Sprintf("%s-%s-agent-principal", argoCDName, ns.Name),
				},
			}
			clusterRoleBinding = &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-%s-agent-principal", argoCDName, ns.Name),
				},
			}

			secretNames = agentFixture.AgentSecretNames{
				JWTSecretName:                  agentJWTSecretName,
				PrincipalTLSSecretName:         agentPrincipalTLSSecretName,
				RootCASecretName:               agentRootCASecretName,
				ResourceProxyTLSSecretName:     agentResourceProxyTLSSecretName,
				RedisInitialPasswordSecretName: "example-redis-initial-password",
			}

			resourceProxyServiceName = fmt.Sprintf("%s-agent-principal-resource-proxy", argoCDName)
			serviceNames = []string{
				argoCDAgentPrincipalName,
				fmt.Sprintf("%s-agent-principal-metrics", argoCDName),
				fmt.Sprintf("%s-redis", argoCDName),
				fmt.Sprintf("%s-repo-server", argoCDName),
				fmt.Sprintf("%s-server", argoCDName),
				fmt.Sprintf("%s-agent-principal-redisproxy", argoCDName),
				resourceProxyServiceName,
				fmt.Sprintf("%s-agent-principal-healthz", argoCDName),
			}
			deploymentNames = []string{
				fmt.Sprintf("%s-redis", argoCDName),
				fmt.Sprintf("%s-repo-server", argoCDName),
				fmt.Sprintf("%s-server", argoCDName),
			}

			principalDeployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      argoCDAgentPrincipalName,
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
			if k8sClient != nil {
				By("Cleanup cluster-scoped resources")
				_ = k8sClient.Delete(ctx, clusterRole)
				_ = k8sClient.Delete(ctx, clusterRoleBinding)
			}

			By("Cleanup namespace")
			if cleanupFunc != nil {
				cleanupFunc()
			}
		})

		verifyExpectedResourcesExist := func() {
			agentFixture.VerifyExpectedResourcesExist(agentFixture.VerifyExpectedResourcesExistParams{
				Namespace:                ns,
				ArgoCDAgentPrincipalName: argoCDAgentPrincipalName,
				ArgoCDName:               argoCDName,
				ServiceAccount:           serviceAccount,
				Role:                     role,
				RoleBinding:              roleBinding,
				ClusterRole:              clusterRole,
				ClusterRoleBinding:       clusterRoleBinding,
				PrincipalDeployment:      principalDeployment,
				SecretNames:              secretNames,
				ServiceNames:             serviceNames,
				PrincipalNetworkPolicy:   principalNetworkPolicy,
				DeploymentNames:          deploymentNames,
				ExpectRoute:              new(false),
			})
		}

		getPrincipalContainer := func() *corev1.Container {
			Eventually(principalDeployment).Should(k8sFixture.ExistByName())
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: argoCDAgentPrincipalName, Namespace: ns.Name}, principalDeployment)).To(Succeed())
			container := deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentPrincipalName, *principalDeployment)
			Expect(container).ToNot(BeNil())
			return container
		}

		networkPolicyHasPort := func(port int32) bool {
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: principalNetworkPolicy.Name, Namespace: ns.Name}, principalNetworkPolicy)).To(Succeed())
			Expect(principalNetworkPolicy.Spec.Ingress).ToNot(BeEmpty())
			for _, p := range principalNetworkPolicy.Spec.Ingress[0].Ports {
				if p.Port != nil && p.Port.IntVal == port {
					return true
				}
			}
			return false
		}

		containerHasHAAdminPort := func(container *corev1.Container, port int32) bool {
			for _, p := range container.Ports {
				if p.Name == "ha-admin" && p.ContainerPort == port {
					return true
				}
			}
			return false
		}

		It("should propagate HA primary configuration to principal deployment env vars, ports, and network policy", func() {
			By("Create ArgoCD instance with HA principal enabled as primary")
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("Verify expected resources are created for principal pod")
			verifyExpectedResourcesExist()

			By("Verify HA environment variables are set correctly")
			container := getPrincipalContainer()
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: argocdagent.EnvArgoCDPrincipalHAEnabled, Value: "true"}))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: argocdagent.EnvArgoCDPrincipalHAPreferredRole, Value: "primary"}))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: argocdagent.EnvArgoCDPrincipalHAPeerAddress, Value: "peer.example.com:443"}))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: argocdagent.EnvArgoCDPrincipalHAAllowedReplicationClients, Value: "argocd-agent-principal-peer"}))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: argocdagent.EnvArgoCDPrincipalHAAdminPort, Value: fmt.Sprintf("%d", argocdagent.PrincipalHAAdminPort)}))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: argocdagent.EnvArgoCDPrincipalHAAdminAuth, Value: "mtls:subject:CN=ha-admin"}))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: argocdagent.EnvArgoCDPrincipalHAReplicationInitialAckTimeout, Value: "10m"}))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: argocdagent.EnvArgoCDPrincipalHAPeerTLSCert, Value: "/app/config/ha-peer-tls/tls.crt"}))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: argocdagent.EnvArgoCDPrincipalHAPeerTLSKey, Value: "/app/config/ha-peer-tls/tls.key"}))

			By("Verify ha-peer-tls secret is mounted")
			Expect(container.VolumeMounts).To(ContainElement(HaveField("Name", "ha-peer-tls")))
			Expect(principalDeployment.Spec.Template.Spec.Volumes).To(ContainElement(HaveField("Name", "ha-peer-tls")))

			By("Verify ha-admin container port is exposed")
			Expect(containerHasHAAdminPort(container, argocdagent.PrincipalHAAdminPort)).To(BeTrue(), "ha-admin port should be present")

			By("Verify NetworkPolicy includes HA admin port")
			Expect(networkPolicyHasPort(argocdagent.PrincipalHAAdminPort)).To(BeTrue(), "network policy should include HA admin port")
		})

		It("should update HA configuration and remove HA settings when disabled", func() {
			By("Create ArgoCD instance with HA principal enabled as primary")
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			verifyExpectedResourcesExist()

			By("Update HA configuration to replica role with custom admin port")
			customAdminPort := int32(9405)
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: argoCDName, Namespace: ns.Name}, argoCD)).To(Succeed())
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ArgoCDAgent.Principal.HA.PreferredRole = "replica"
				ac.Spec.ArgoCDAgent.Principal.HA.PeerAddress = "primary.example.com:443"
				ac.Spec.ArgoCDAgent.Principal.HA.AllowedReplicationClients = []string{"primary-identity"}
				ac.Spec.ArgoCDAgent.Principal.HA.AdminPort = &customAdminPort
				ac.Spec.ArgoCDAgent.Principal.HA.AdminAuth = "mtls:subject:CN=ha-admin-updated"
				ac.Spec.ArgoCDAgent.Principal.HA.ReplicationInitialAckTimeout = "15m"
			})

			By("Wait for updated HA env vars and port to propagate")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: argoCDAgentPrincipalName, Namespace: ns.Name}, principalDeployment)
				if err != nil {
					return false
				}
				container := deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentPrincipalName, *principalDeployment)
				if container == nil {
					return false
				}
				hasRole, hasPortEnv, hasAuth, hasTimeout := false, false, false, false
				for _, env := range container.Env {
					switch env.Name {
					case argocdagent.EnvArgoCDPrincipalHAPreferredRole:
						hasRole = env.Value == "replica"
					case argocdagent.EnvArgoCDPrincipalHAAdminPort:
						hasPortEnv = env.Value == "9405"
					case argocdagent.EnvArgoCDPrincipalHAAdminAuth:
						hasAuth = env.Value == "mtls:subject:CN=ha-admin-updated"
					case argocdagent.EnvArgoCDPrincipalHAReplicationInitialAckTimeout:
						hasTimeout = env.Value == "15m"
					}
				}
				return hasRole && hasPortEnv && hasAuth && hasTimeout && containerHasHAAdminPort(container, 9405)
			}, "120s", "5s").Should(BeTrue(), "HA configuration updates should propagate to the principal deployment")

			container := getPrincipalContainer()
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: argocdagent.EnvArgoCDPrincipalHAEnabled, Value: "true"}))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: argocdagent.EnvArgoCDPrincipalHAPreferredRole, Value: "replica"}))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: argocdagent.EnvArgoCDPrincipalHAPeerAddress, Value: "primary.example.com:443"}))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: argocdagent.EnvArgoCDPrincipalHAAllowedReplicationClients, Value: "primary-identity"}))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: argocdagent.EnvArgoCDPrincipalHAAdminPort, Value: "9405"}))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: argocdagent.EnvArgoCDPrincipalHAAdminAuth, Value: "mtls:subject:CN=ha-admin-updated"}))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: argocdagent.EnvArgoCDPrincipalHAReplicationInitialAckTimeout, Value: "15m"}))
			Expect(containerHasHAAdminPort(container, 9405)).To(BeTrue())
			Expect(networkPolicyHasPort(9405)).To(BeTrue(), "network policy should include updated HA admin port 9405")

			By("Disable HA on the principal")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: argoCDName, Namespace: ns.Name}, argoCD)).To(Succeed())
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ArgoCDAgent.Principal.HA.Enabled = new(false)
			})

			By("Wait for HA env vars, peer TLS volume, and ports to be removed")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: argoCDAgentPrincipalName, Namespace: ns.Name}, principalDeployment)
				if err != nil {
					return false
				}
				container := deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentPrincipalName, *principalDeployment)
				if container == nil {
					return false
				}
				for _, env := range container.Env {
					if env.Name == argocdagent.EnvArgoCDPrincipalHAEnabled {
						return false
					}
				}
				for _, p := range container.Ports {
					if p.Name == "ha-admin" {
						return false
					}
				}
				for _, m := range container.VolumeMounts {
					if m.Name == "ha-peer-tls" {
						return false
					}
				}
				return true
			}, "120s", "5s").Should(BeTrue(), "HA env vars, ha-peer-tls mount, and ha-admin port should be removed when HA is disabled")

			container = getPrincipalContainer()
			for _, env := range container.Env {
				Expect(env.Name).ToNot(HavePrefix("ARGOCD_PRINCIPAL_HA_"), "HA env var %s should not be present when HA is disabled", env.Name)
			}
			Expect(container.VolumeMounts).ToNot(ContainElement(HaveField("Name", "ha-peer-tls")))
			Expect(principalDeployment.Spec.Template.Spec.Volumes).ToNot(ContainElement(HaveField("Name", "ha-peer-tls")))
			Expect(containerHasHAAdminPort(container, 9405)).To(BeFalse())
			Expect(containerHasHAAdminPort(container, argocdagent.PrincipalHAAdminPort)).To(BeFalse())

			By("Verify NetworkPolicy no longer includes HA admin ports")
			Eventually(func() bool {
				return !networkPolicyHasPort(9405) && !networkPolicyHasPort(argocdagent.PrincipalHAAdminPort)
			}, "60s", "5s").Should(BeTrue(), "network policy should not include HA admin ports when HA is disabled")
		})
	})
})
