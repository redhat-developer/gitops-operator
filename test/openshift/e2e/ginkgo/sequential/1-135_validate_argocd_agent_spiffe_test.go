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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argov1beta1api "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argocdagent"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argocdagent/agent"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
)

const (
	// SPIFFE-related constants
	spiffeCSIDriverDefault = "csi.spiffe.io"
	spiffeCSIDriverCustom  = "custom.csi.spiffe.io"
	spiffeVolumeName       = "spiffe-workload-api"
	spiffeSocketMountPath  = "/run/spire/sockets"
	spiffeSocketURI        = "unix:///run/spire/sockets/spire-agent.sock"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {
	const (
		argoCDName               = "example"
		argoCDAgentPrincipalName = "example-agent-principal"
		argoCDAgentAgentName     = "example-agent-agent"
	)

	Context("1-135_validate_argocd_agent_spiffe", func() {
		var (
			k8sClient           client.Client
			ctx                 context.Context
			argoCD              *argov1beta1api.ArgoCD
			ns                  *corev1.Namespace
			cleanupFunc         func()
			principalDeployment *appsv1.Deployment
			agentDeployment     *appsv1.Deployment
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ARGOCD_CLUSTER_CONFIG_NAMESPACES", "openshift-gitops, argocd-spiffe-1-135")

			ctx = context.Background()
			ns, cleanupFunc = fixture.CreateNamespaceWithCleanupFunc("argocd-spiffe-1-135")

			principalDeployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      argoCDAgentPrincipalName,
					Namespace: ns.Name,
				},
			}

			agentDeployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      argoCDAgentAgentName,
					Namespace: ns.Name,
				},
			}
		})

		AfterEach(func() {
			By("Cleanup namespace")
			if cleanupFunc != nil {
				cleanupFunc()
			}
		})

		// createBasePrincipalArgoCD creates a base ArgoCD CR with principal enabled (without SPIFFE)
		createBasePrincipalArgoCD := func() *argov1beta1api.ArgoCD {
			return &argov1beta1api.ArgoCD{
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
						},
					},
				},
			}
		}

		// createBaseAgentArgoCD creates a base ArgoCD CR with agent enabled (without SPIFFE)
		createBaseAgentArgoCD := func() *argov1beta1api.ArgoCD {
			return &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      argoCDName,
					Namespace: ns.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{
					Controller: argov1beta1api.ArgoCDApplicationControllerSpec{
						Enabled: new(false),
					},
					Server: argov1beta1api.ArgoCDServerSpec{
						Enabled: new(false),
					},
					ArgoCDAgent: &argov1beta1api.ArgoCDAgentSpec{
						Agent: &argov1beta1api.AgentSpec{
							Enabled:  new(true),
							Creds:    "mtls:any",
							LogLevel: "info",
							Client: &argov1beta1api.AgentClientSpec{
								PrincipalServerAddress: "argocd-agent-principal.example.com",
								PrincipalServerPort:    "443",
								Mode:                   string(argov1beta1api.AgentModeManaged),
							},
							TLS: &argov1beta1api.AgentTLSSpec{
								SecretName:       "argocd-agent-client-tls",
								RootCASecretName: "argocd-agent-ca",
							},
							Redis: &argov1beta1api.AgentRedisSpec{
								ServerAddress: fmt.Sprintf("%s-%s:%d", argoCDName, "redis", common.ArgoCDDefaultRedisPort),
							},
						},
					},
				},
			}
		}

		It("should configure principal SPIFFE with JWT auth method, verify defaults, and custom CSI driver", func() {
			By("Create ArgoCD instance with SPIFFE enabled using default JWT auth (no AuthMethod specified)")

			argoCD = createBasePrincipalArgoCD()
			argoCD.Spec.ArgoCDAgent.Principal.SPIFFE = &argov1beta1api.SPIFFESpec{
				Enabled: true,
				// AuthMethod not specified - should default to "jwt"
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("Wait for principal deployment to be created")
			Eventually(principalDeployment, "60s", "2s").Should(k8sFixture.ExistByName())

			By("Verify default JWT auth method is used when AuthMethod is not specified")
			container := deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentPrincipalName, *principalDeployment)
			Expect(container).ToNot(BeNil())

			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  argocdagent.EnvArgoCDPrincipalSpireAgentSocket,
				Value: spiffeSocketURI,
			}), "ARGOCD_PRINCIPAL_SPIRE_AGENT_SOCKET should be set")

			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  argocdagent.EnvArgoCDPrincipalSpireAuthMethod,
				Value: common.SpiffeAuthMethodJWT,
			}), "Should default to 'jwt' auth method")

			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  argocdagent.EnvArgoCDPrincipalAuth,
				Value: "spiffe-jwt:spiffe://[^/]+/ns/[^/]+/argocd/agent/(.+)",
			}), "Should use JWT-based SPIFFE auth pattern")

			By("Verify SPIFFE CSI volume is configured with default driver")
			verifySpiffeVolumeAndMount(principalDeployment, container, spiffeCSIDriverDefault)

			By("Update to use custom CSI driver")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: argoCDName, Namespace: ns.Name}, argoCD)).To(Succeed())
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ArgoCDAgent.Principal.SPIFFE.CSIDriverName = spiffeCSIDriverCustom
			})

			By("Verify custom CSI driver is applied")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: argoCDAgentPrincipalName, Namespace: ns.Name}, principalDeployment)
				if err != nil {
					return false
				}
				for _, vol := range principalDeployment.Spec.Template.Spec.Volumes {
					if vol.Name == spiffeVolumeName && vol.CSI != nil {
						return vol.CSI.Driver == spiffeCSIDriverCustom
					}
				}
				return false
			}, "120s", "5s").Should(BeTrue(), "Custom CSI driver should be applied")
		})

		It("should configure principal SPIFFE with mTLS auth method and allow explicit auth override", func() {
			By("Create ArgoCD instance with SPIFFE mTLS mode")

			argoCD = createBasePrincipalArgoCD()
			argoCD.Spec.ArgoCDAgent.Principal.SPIFFE = &argov1beta1api.SPIFFESpec{
				Enabled:    true,
				AuthMethod: common.SpiffeAuthMethodMTLS,
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("Wait for principal deployment and verify mTLS configuration")
			Eventually(principalDeployment, "60s", "2s").Should(k8sFixture.ExistByName())

			container := deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentPrincipalName, *principalDeployment)
			Expect(container).ToNot(BeNil())

			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  argocdagent.EnvArgoCDPrincipalSpireAuthMethod,
				Value: common.SpiffeAuthMethodMTLS,
			}), "Should use 'mtls' auth method")

			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  argocdagent.EnvArgoCDPrincipalAuth,
				Value: "mtls:uri:spiffe://[^/]+/ns/[^/]+/argocd/agent/(.+)",
			}), "Should use mTLS URI pattern for SPIFFE")

			verifySpiffeVolumeAndMount(principalDeployment, container, spiffeCSIDriverDefault)

			By("Override with explicit auth pattern")
			customAuthPattern := "mtls:CN=([^,]+)"
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: argoCDName, Namespace: ns.Name}, argoCD)).To(Succeed())
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ArgoCDAgent.Principal.Auth = customAuthPattern
			})

			By("Verify explicit auth pattern takes precedence")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: argoCDAgentPrincipalName, Namespace: ns.Name}, principalDeployment)
				if err != nil {
					return false
				}
				container = deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentPrincipalName, *principalDeployment)
				if container == nil {
					return false
				}
				for _, env := range container.Env {
					if env.Name == argocdagent.EnvArgoCDPrincipalAuth {
						return env.Value == customAuthPattern
					}
				}
				return false
			}, "120s", "5s").Should(BeTrue(), "Explicit auth should take precedence")

			// SPIFFE socket should still be configured
			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  argocdagent.EnvArgoCDPrincipalSpireAgentSocket,
				Value: spiffeSocketURI,
			}), "SPIFFE socket should still be configured even with explicit auth")
		})

		It("should handle principal SPIFFE enable, auth method update, and disable lifecycle", func() {
			By("Create ArgoCD instance without SPIFFE")

			argoCD = createBasePrincipalArgoCD()
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("Verify deployment created without SPIFFE configuration")
			Eventually(principalDeployment, "60s", "2s").Should(k8sFixture.ExistByName())

			container := deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentPrincipalName, *principalDeployment)
			Expect(container).ToNot(BeNil())
			verifySpiffeEnvVarsAbsent(container)
			verifySpiffeVolumeAbsent(principalDeployment)

			By("Enable SPIFFE with JWT mode")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: argoCDName, Namespace: ns.Name}, argoCD)).To(Succeed())
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ArgoCDAgent.Principal.SPIFFE = &argov1beta1api.SPIFFESpec{
					Enabled:    true,
					AuthMethod: common.SpiffeAuthMethodJWT,
				}
			})

			By("Verify SPIFFE JWT configuration is added")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: argoCDAgentPrincipalName, Namespace: ns.Name}, principalDeployment)
				if err != nil {
					return false
				}
				container = deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentPrincipalName, *principalDeployment)
				if container == nil {
					return false
				}
				for _, env := range container.Env {
					if env.Name == argocdagent.EnvArgoCDPrincipalSpireAuthMethod && env.Value == common.SpiffeAuthMethodJWT {
						return true
					}
				}
				return false
			}, "120s", "5s").Should(BeTrue(), "SPIFFE JWT should be enabled")

			By("Update auth method from JWT to mTLS")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: argoCDName, Namespace: ns.Name}, argoCD)).To(Succeed())
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ArgoCDAgent.Principal.SPIFFE.AuthMethod = common.SpiffeAuthMethodMTLS
			})

			By("Verify auth method changed to mTLS")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: argoCDAgentPrincipalName, Namespace: ns.Name}, principalDeployment)
				if err != nil {
					return false
				}
				container = deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentPrincipalName, *principalDeployment)
				if container == nil {
					return false
				}
				for _, env := range container.Env {
					if env.Name == argocdagent.EnvArgoCDPrincipalSpireAuthMethod {
						return env.Value == common.SpiffeAuthMethodMTLS
					}
				}
				return false
			}, "120s", "5s").Should(BeTrue(), "Auth method should change to mTLS")

			container = deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentPrincipalName, *principalDeployment)
			Expect(container).ToNot(BeNil())

			// Verify auth pattern also updated
			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  argocdagent.EnvArgoCDPrincipalAuth,
				Value: "mtls:uri:spiffe://[^/]+/ns/[^/]+/argocd/agent/(.+)",
			}), "Auth pattern should update to mTLS URI")

			By("Disable SPIFFE")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: argoCDName, Namespace: ns.Name}, argoCD)).To(Succeed())
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ArgoCDAgent.Principal.SPIFFE.Enabled = false
			})

			By("Verify SPIFFE configuration is removed")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: argoCDAgentPrincipalName, Namespace: ns.Name}, principalDeployment)
				if err != nil {
					return false
				}
				container = deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentPrincipalName, *principalDeployment)
				if container == nil {
					return false
				}
				for _, env := range container.Env {
					if env.Name == argocdagent.EnvArgoCDPrincipalSpireAgentSocket {
						return false // Still present
					}
				}
				return true
			}, "120s", "5s").Should(BeTrue(), "SPIFFE should be removed")

			container = deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentPrincipalName, *principalDeployment)
			Expect(container).ToNot(BeNil())

			verifySpiffeVolumeAbsent(principalDeployment)

			// Verify fallback to default mTLS auth
			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  argocdagent.EnvArgoCDPrincipalAuth,
				Value: "mtls:CN=([^,]+)",
			}), "Should fall back to default mTLS auth")
		})

		It("should configure agent SPIFFE with JWT and mTLS modes, and custom CSI driver", func() {
			By("Create ArgoCD instance with agent SPIFFE JWT mode (default)")

			argoCD = createBaseAgentArgoCD()
			argoCD.Spec.ArgoCDAgent.Agent.SPIFFE = &argov1beta1api.SPIFFESpec{
				Enabled: true,
				// AuthMethod defaults to "jwt"
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("Verify agent deployment with default JWT configuration")
			Eventually(agentDeployment, "60s", "2s").Should(k8sFixture.ExistByName())

			container := deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentAgentName, *agentDeployment)
			Expect(container).ToNot(BeNil())

			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  agent.EnvArgoCDAgentSpireAgentSocket,
				Value: spiffeSocketURI,
			}), "Agent SPIFFE socket should be set")

			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  agent.EnvArgoCDAgentSpireAuthMethod,
				Value: common.SpiffeAuthMethodJWT,
			}), "Agent should default to JWT")

			verifySpiffeVolumeAndMount(agentDeployment, container, spiffeCSIDriverDefault)

			By("Update to mTLS mode with custom CSI driver")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: argoCDName, Namespace: ns.Name}, argoCD)).To(Succeed())
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ArgoCDAgent.Agent.SPIFFE.AuthMethod = common.SpiffeAuthMethodMTLS
				ac.Spec.ArgoCDAgent.Agent.SPIFFE.CSIDriverName = spiffeCSIDriverCustom
			})

			By("Verify mTLS mode and custom CSI driver")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: argoCDAgentAgentName, Namespace: ns.Name}, agentDeployment)
				if err != nil {
					return false
				}
				container = deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentAgentName, *agentDeployment)
				if container == nil {
					return false
				}
				// Check auth method
				authMethodOK := false
				for _, env := range container.Env {
					if env.Name == agent.EnvArgoCDAgentSpireAuthMethod && env.Value == common.SpiffeAuthMethodMTLS {
						authMethodOK = true
						break
					}
				}
				// Check CSI driver
				csiDriverOK := false
				for _, vol := range agentDeployment.Spec.Template.Spec.Volumes {
					if vol.Name == spiffeVolumeName && vol.CSI != nil && vol.CSI.Driver == spiffeCSIDriverCustom {
						csiDriverOK = true
						break
					}
				}
				return authMethodOK && csiDriverOK
			}, "120s", "5s").Should(BeTrue(), "Agent should have mTLS mode with custom CSI driver")
		})

		It("should handle agent SPIFFE enable and disable lifecycle", func() {
			By("Create ArgoCD instance without agent SPIFFE")

			argoCD = createBaseAgentArgoCD()
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("Verify agent deployment without SPIFFE")
			Eventually(agentDeployment, "60s", "2s").Should(k8sFixture.ExistByName())

			container := deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentAgentName, *agentDeployment)
			Expect(container).ToNot(BeNil())
			verifyAgentSpiffeEnvVarsAbsent(container)
			verifySpiffeVolumeAbsent(agentDeployment)

			By("Enable SPIFFE on agent")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: argoCDName, Namespace: ns.Name}, argoCD)).To(Succeed())
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ArgoCDAgent.Agent.SPIFFE = &argov1beta1api.SPIFFESpec{
					Enabled:    true,
					AuthMethod: common.SpiffeAuthMethodJWT,
				}
			})

			By("Verify SPIFFE is enabled on agent")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: argoCDAgentAgentName, Namespace: ns.Name}, agentDeployment)
				if err != nil {
					return false
				}
				container = deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentAgentName, *agentDeployment)
				if container == nil {
					return false
				}
				for _, env := range container.Env {
					if env.Name == agent.EnvArgoCDAgentSpireAgentSocket {
						return env.Value == spiffeSocketURI
					}
				}
				return false
			}, "120s", "5s").Should(BeTrue(), "Agent SPIFFE should be enabled")

			container = deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentAgentName, *agentDeployment)
			Expect(container).ToNot(BeNil())

			verifySpiffeVolumeAndMount(agentDeployment, container, spiffeCSIDriverDefault)

			By("Disable SPIFFE on agent")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: argoCDName, Namespace: ns.Name}, argoCD)).To(Succeed())
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ArgoCDAgent.Agent.SPIFFE.Enabled = false
			})

			By("Verify SPIFFE is disabled on agent")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: argoCDAgentAgentName, Namespace: ns.Name}, agentDeployment)
				if err != nil {
					return false
				}
				container = deploymentFixture.GetTemplateSpecContainerByName(argoCDAgentAgentName, *agentDeployment)
				if container == nil {
					return false
				}
				for _, env := range container.Env {
					if env.Name == agent.EnvArgoCDAgentSpireAgentSocket {
						return false // Still present
					}
				}
				return true
			}, "120s", "5s").Should(BeTrue(), "Agent SPIFFE should be disabled")

			verifySpiffeVolumeAbsent(agentDeployment)
		})
	})
})

func verifySpiffeVolumeAndMount(deployment *appsv1.Deployment, container *corev1.Container, expectedCSIDriver string) {
	By("Verifying SPIFFE volume mount exists")

	var spiffeMount *corev1.VolumeMount
	for i := range container.VolumeMounts {
		if container.VolumeMounts[i].Name == spiffeVolumeName {
			spiffeMount = &container.VolumeMounts[i]
			break
		}
	}
	Expect(spiffeMount).ToNot(BeNil(), "SPIFFE volume mount should exist")
	Expect(spiffeMount.MountPath).To(Equal(spiffeSocketMountPath), "SPIFFE mount path should be correct")
	Expect(spiffeMount.ReadOnly).To(BeTrue(), "SPIFFE volume mount should be read-only")

	By("Verifying SPIFFE CSI volume exists")

	var spiffeVolume *corev1.Volume
	for i := range deployment.Spec.Template.Spec.Volumes {
		if deployment.Spec.Template.Spec.Volumes[i].Name == spiffeVolumeName {
			spiffeVolume = &deployment.Spec.Template.Spec.Volumes[i]
			break
		}
	}
	Expect(spiffeVolume).ToNot(BeNil(), "SPIFFE volume should exist")
	Expect(spiffeVolume.CSI).ToNot(BeNil(), "SPIFFE volume should be a CSI volume")
	Expect(spiffeVolume.CSI.Driver).To(Equal(expectedCSIDriver), "SPIFFE CSI driver should match expected value")
	Expect(spiffeVolume.CSI.ReadOnly).ToNot(BeNil(), "SPIFFE CSI ReadOnly should be set")
	Expect(*spiffeVolume.CSI.ReadOnly).To(BeTrue(), "SPIFFE CSI volume should be read-only")
}

func verifySpiffeVolumeAbsent(deployment *appsv1.Deployment) {
	By("Verifying SPIFFE volume does not exist")

	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		Expect(volume.Name).ToNot(Equal(spiffeVolumeName), "SPIFFE volume should not exist")
	}
}

func verifySpiffeEnvVarsAbsent(container *corev1.Container) {
	By("Verifying SPIFFE environment variables are not present")

	for _, env := range container.Env {
		Expect(env.Name).ToNot(Equal(argocdagent.EnvArgoCDPrincipalSpireAgentSocket),
			"ARGOCD_PRINCIPAL_SPIRE_AGENT_SOCKET should not be set")
		Expect(env.Name).ToNot(Equal(argocdagent.EnvArgoCDPrincipalSpireAuthMethod),
			"ARGOCD_PRINCIPAL_SPIRE_AUTH_METHOD should not be set")
	}
}

func verifyAgentSpiffeEnvVarsAbsent(container *corev1.Container) {
	By("Verifying agent SPIFFE environment variables are not present")

	for _, env := range container.Env {
		Expect(env.Name).ToNot(Equal(agent.EnvArgoCDAgentSpireAgentSocket),
			"ARGOCD_AGENT_SPIRE_AGENT_SOCKET should not be set")
		Expect(env.Name).ToNot(Equal(agent.EnvArgoCDAgentSpireAuthMethod),
			"ARGOCD_AGENT_SPIRE_AUTH_METHOD should not be set")
	}
}
