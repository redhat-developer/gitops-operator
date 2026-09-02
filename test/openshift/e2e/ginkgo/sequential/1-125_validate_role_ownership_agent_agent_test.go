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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	"github.com/argoproj-labs/argocd-operator/common"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	const (
		clusterRoleName        = "argocd-agent-argocd-agent-agent-agent"
		clusterRoleBindingName = "argocd-agent-argocd-agent-agent-agent"
	)

	Context("1-125_validate_role_ownership_agent_agent", func() {
		var (
			k8sClient       client.Client
			ctx             context.Context
			argoCD          *argov1beta1api.ArgoCD
			ns              *corev1.Namespace
			cleanupFunc     func()
			serviceNames    []string
			agentDeployment *appsv1.Deployment
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ARGOCD_CLUSTER_CONFIG_NAMESPACES", "openshift-gitops, argocd-agent")

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
			ns, cleanupFunc = fixture.CreateNamespaceWithCleanupFunc("argocd-agent")

			// Define ArgoCD CR with agent enabled
			argoCD = &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-agent",
					Namespace: ns.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{
					Controller: argov1beta1api.ArgoCDApplicationControllerSpec{
						Enabled: ptr.To(false),
					},
					Server: argov1beta1api.ArgoCDServerSpec{
						Enabled: ptr.To(false),
					},
					ArgoCDAgent: &argov1beta1api.ArgoCDAgentSpec{
						Agent: &argov1beta1api.AgentSpec{
							Enabled:   ptr.To(true),
							Creds:     "mtls:any",
							LogLevel:  "info",
							LogFormat: "text",
							Client: &argov1beta1api.AgentClientSpec{
								PrincipalServerAddress: "argocd-agent-principal.example.com",
								PrincipalServerPort:    "443",
								Mode:                   string(argov1beta1api.AgentModeManaged),
								EnableWebSocket:        ptr.To(false),
								EnableCompression:      ptr.To(false),
								KeepAliveInterval:      "30s",
							},
							TLS: &argov1beta1api.AgentTLSSpec{
								SecretName:       agentClientTLSSecretName,
								RootCASecretName: agentRootCASecretName,
								Insecure:         ptr.To(false),
							},
							Redis: &argov1beta1api.AgentRedisSpec{
								ServerAddress: fmt.Sprintf("%s-%s:%d", "argocd-agent", "redis", common.ArgoCDDefaultRedisPort),
							},
						},
					},
				},
			}

			serviceNames = []string{
				"argocd-agent-agent-agent-metrics",
				"argocd-agent-agent-agent-healthz",
				"argocd-agent-redis",
			}

			agentDeployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-agent-agent-agent",
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

		verifyExpectedResourcesExist := func(ns *corev1.Namespace) {

			By("verifying expected resources exist")
			for _, serviceName := range serviceNames {

				By("verifying Service '" + serviceName + "' exists and is a LoadBalancer or ClusterIP depending on which service")

				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      serviceName,
						Namespace: ns.Name,
					},
				}
				Eventually(service).Should(k8sFixture.ExistByName())

			}
			By("verifying primary agent Deployment has expected labels")

			Eventually(agentDeployment).Should(k8sFixture.ExistByName())
			Eventually(agentDeployment).Should(k8sFixture.HaveLabelWithValue("app.kubernetes.io/component", string(argov1beta1api.AgentComponentTypeAgent)))
			Eventually(agentDeployment).Should(k8sFixture.HaveLabelWithValue("app.kubernetes.io/managed-by", "argocd-agent"))
			Eventually(agentDeployment).Should(k8sFixture.HaveLabelWithValue("app.kubernetes.io/name", "argocd-agent-agent-agent"))
			Eventually(agentDeployment).Should(k8sFixture.HaveLabelWithValue("app.kubernetes.io/part-of", "argocd-agent"))

		}

		It("validates that namespace-scoped resources do not delete a ClusterRole or ClusterRoleBinding with a matching generated name for Agent", func() {

			By("creating ArgoCD instance with agent enabled")
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying expected resources are created with correct values")
			verifyExpectedResourcesExist(ns)

			By("verifying ClusterRole and ClusterRoleBinding for agent exist with correct names")

			clusterRole := &rbacv1.ClusterRole{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: clusterRoleName}, clusterRole)
			}).Should(Succeed())
			initialClusterRoleUid := clusterRole.GetUID()

			clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: clusterRoleBindingName}, clusterRoleBinding)
			}).Should(Succeed())
			initialClusterRoleBindingUid := clusterRoleBinding.GetUID()

			By("Create namespace-scoped ArgoCD instance namespace")
			nsScoped, cleanupFuncScoped := fixture.CreateNamespaceWithCleanupFunc("agent")
			defer cleanupFuncScoped()

			argoCD1 := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-agent-argocd",
					Namespace: nsScoped.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{
					Controller: argov1beta1api.ArgoCDApplicationControllerSpec{
						Enabled: ptr.To(false),
					},
					Server: argov1beta1api.ArgoCDServerSpec{
						Enabled: ptr.To(false),
					},
					ArgoCDAgent: &argov1beta1api.ArgoCDAgentSpec{
						Agent: &argov1beta1api.AgentSpec{
							Enabled:   ptr.To(true),
							Creds:     "mtls:any",
							LogLevel:  "info",
							LogFormat: "text",
							Client: &argov1beta1api.AgentClientSpec{
								PrincipalServerAddress: "argocd-agent-principal.example.com",
								PrincipalServerPort:    "443",
								Mode:                   string(argov1beta1api.AgentModeManaged),
								EnableWebSocket:        ptr.To(false),
								EnableCompression:      ptr.To(false),
								KeepAliveInterval:      "30s",
							},
							TLS: &argov1beta1api.AgentTLSSpec{
								SecretName:       agentClientTLSSecretName,
								RootCASecretName: agentRootCASecretName,
								Insecure:         ptr.To(false),
							},
							Redis: &argov1beta1api.AgentRedisSpec{
								ServerAddress: fmt.Sprintf("%s-%s:%d", "argocd-agent-argocd", "redis", common.ArgoCDDefaultRedisPort),
							},
						},
					},
				},
			}

			By("Create namespace-scoped ArgoCD instance with agent")

			Expect(k8sClient.Create(ctx, argoCD1)).To(Succeed())
			Eventually(argoCD1, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("Verifying UID of ClusterRole and ClusterRoleBinding to ensure they are not deleted by namespaced scoped resources")
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

			Expect(afterReconcileClusterRoleUid).To(Equal(initialClusterRoleUid))
			Expect(afterReconcileClusterRBUid).To(Equal(initialClusterRoleBindingUid))

		})

	})

})
