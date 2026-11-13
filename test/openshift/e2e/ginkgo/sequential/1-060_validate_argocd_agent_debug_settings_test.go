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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argov1beta1api "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argocdagent"
	agentutil "github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argocdagent/util"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	configmapFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/configmap"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-060_validate_argocd_agent_debug_settings", func() {

		const (
			argoCDName               = "example"
			argoCDAgentPrincipalName = "example-agent-principal"
		)

		var (
			k8sClient           client.Client
			ctx                 context.Context
			ns                  *corev1.Namespace
			cleanupFunc         func()
			argoCD              *argov1beta1api.ArgoCD
			paramsConfigMap     *corev1.ConfigMap
			principalDeployment *appsv1.Deployment
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()

			ns, cleanupFunc = fixture.CreateNamespaceWithCleanupFunc("argocd-agent-debug-1-060")

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
							Enabled: new(true),
							Auth:    "mtls:CN=([^,]+)",
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

			paramsConfigMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.ArgoCDAgentParamsConfigMapName,
					Namespace: ns.Name,
				},
			}

			principalDeployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      argoCDAgentPrincipalName,
					Namespace: ns.Name,
				},
			}
		})

		AfterEach(func() {
			if cleanupFunc != nil {
				cleanupFunc()
			}
		})

		// currentGeneration returns the principal Deployment's metadata.generation, which the API
		// server bumps on every change to the Deployment spec — including any change to the pod
		// template. It is therefore the signal that a pod restart was (or was not) triggered.
		currentGeneration := func() int64 {
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(principalDeployment), deployment)).To(Succeed())
			return deployment.Generation
		}

		podTemplate := func() corev1.PodTemplateSpec {
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(principalDeployment), deployment)).To(Succeed())
			return deployment.Spec.Template
		}

		It("should serve the runtime-switchable settings from the params ConfigMap and toggle them without restarting the principal", func() {

			By("creating an ArgoCD instance with the principal enabled")
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("verifying the operator creates the agent params ConfigMap")
			Eventually(paramsConfigMap, "90s", "5s").Should(k8sFixture.ExistByName())

			By("verifying every managed key is written, at the principal's own defaults")
			// Each key must always be present: the pod spec sources them with a
			// ConfigMapKeyRef, and argocd-agent treats its environment as the baseline for any
			// key that is missing - so a key that could disappear would strand the principal on
			// whatever value it happened to start with.
			Eventually(paramsConfigMap, "90s", "5s").Should(SatisfyAll(
				configmapFixture.HaveStringDataKeyValue(common.ArgoCDAgentPrincipalLogLevelKey, common.ArgoCDAgentDefaultLogLevel),
				configmapFixture.HaveStringDataKeyValue(common.ArgoCDAgentPrincipalFullDetailKey, ""),
				configmapFixture.HaveStringDataKeyValue(common.ArgoCDAgentPrincipalPProfEnabledKey, "false"),
			))

			By("verifying the principal sources the runtime-switchable settings from the ConfigMap")
			Eventually(principalDeployment, "90s", "5s").Should(k8sFixture.ExistByName())

			template := podTemplate()
			Expect(template.Spec.Containers).To(HaveLen(1))
			container := template.Spec.Containers[0]

			for envName, cmKey := range map[string]string{
				argocdagent.EnvArgoCDPrincipalLogLevel:     common.ArgoCDAgentPrincipalLogLevelKey,
				argocdagent.EnvArgoCDPrincipalFullDetail:   common.ArgoCDAgentPrincipalFullDetailKey,
				argocdagent.EnvArgoCDPrincipalPProfEnabled: common.ArgoCDAgentPrincipalPProfEnabledKey,
			} {
				Expect(container.Env).To(ContainElement(corev1.EnvVar{
					Name:      envName,
					ValueFrom: agentutil.AgentParamsEnvVarSource(cmKey),
				}), "%s must be sourced from the params ConfigMap, not set to a literal", envName)
			}

			By("waiting for the principal deployment to settle before recording a baseline")
			// The operator may still be converging the deployment right after creating it.
			// Recording the baseline mid-convergence would make the no-restart assertion below
			// flaky, so wait until the generation stops moving first.
			var generationBefore int64
			Eventually(func() bool {
				generationBefore = currentGeneration()
				time.Sleep(5 * time.Second)
				return currentGeneration() == generationBefore
			}, "180s", "5s").Should(BeTrue(), "the principal deployment generation should stop changing")

			templateBefore := podTemplate()

			By("enabling the debug settings via the ArgoCD CR")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCD), argoCD)).To(Succeed())
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ArgoCDAgent.Principal.LogLevel = "trace"
				ac.Spec.ArgoCDAgent.Principal.LogFullDetail = []string{"events", "informers"}
				ac.Spec.ArgoCDAgent.Principal.PProf = &argov1beta1api.ArgoCDAgentPProfSpec{
					Enabled: new(true),
				}
			})

			By("verifying the settings land in the params ConfigMap")
			Eventually(paramsConfigMap, "90s", "5s").Should(SatisfyAll(
				configmapFixture.HaveStringDataKeyValue(common.ArgoCDAgentPrincipalLogLevelKey, "trace"),
				configmapFixture.HaveStringDataKeyValue(common.ArgoCDAgentPrincipalFullDetailKey, "events,informers"),
				configmapFixture.HaveStringDataKeyValue(common.ArgoCDAgentPrincipalPProfEnabledKey, "true"),
			))

			// This is the assertion that actually protects the feature. The whole point of
			// routing these settings through a ConfigMap is that toggling them must not roll the
			// principal: a restart would make every connected agent reconnect at once, which is
			// exactly what an operator is trying to avoid while debugging a live problem.
			By("verifying the principal was NOT restarted")
			Consistently(currentGeneration, "30s", "5s").Should(Equal(generationBefore),
				"toggling the debug settings must not change the principal deployment spec")
			Expect(podTemplate()).To(Equal(templateBefore),
				"the principal pod template must be unchanged after toggling the debug settings")

			By("unsetting the debug settings and verifying the keys revert to defaults")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCD), argoCD)).To(Succeed())
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ArgoCDAgent.Principal.LogLevel = ""
				ac.Spec.ArgoCDAgent.Principal.LogFullDetail = nil
				ac.Spec.ArgoCDAgent.Principal.PProf = nil
			})

			Eventually(paramsConfigMap, "90s", "5s").Should(SatisfyAll(
				configmapFixture.HaveStringDataKeyValue(common.ArgoCDAgentPrincipalLogLevelKey, common.ArgoCDAgentDefaultLogLevel),
				configmapFixture.HaveStringDataKeyValue(common.ArgoCDAgentPrincipalFullDetailKey, ""),
				configmapFixture.HaveStringDataKeyValue(common.ArgoCDAgentPrincipalPProfEnabledKey, "false"),
			))

			By("verifying the principal still was not restarted across the whole sequence")
			Expect(currentGeneration()).To(Equal(generationBefore),
				"no runtime-switchable setting may ever alter the principal deployment spec")
		})

		It("should revert a manual edit of an operator-managed key but preserve unmanaged keys", func() {

			By("creating an ArgoCD instance with a debug log level")
			argoCD.Spec.ArgoCDAgent.Principal.LogLevel = "debug"
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			Eventually(paramsConfigMap, "90s", "5s").Should(
				configmapFixture.HaveStringDataKeyValue(common.ArgoCDAgentPrincipalLogLevelKey, "debug"))

			By("hand-editing a managed key and adding an unmanaged one")
			configmapFixture.Update(paramsConfigMap, func(cm *corev1.ConfigMap) {
				cm.Data[common.ArgoCDAgentPrincipalLogLevelKey] = "trace"
				cm.Data["principal.pprof.port"] = "6060"
			})

			By("verifying the ArgoCD CR wins for the managed key")
			Eventually(paramsConfigMap, "90s", "5s").Should(
				configmapFixture.HaveStringDataKeyValue(common.ArgoCDAgentPrincipalLogLevelKey, "debug"))

			By("verifying the unmanaged key survives")
			// The operator must not destroy keys it does not own, so a newer upstream setting is
			// still usable before the operator grows a field for it.
			Consistently(paramsConfigMap, "20s", "5s").Should(
				configmapFixture.HaveStringDataKeyValue("principal.pprof.port", "6060"))
		})

		It("should delete the params ConfigMap when the principal is disabled", func() {

			By("creating an ArgoCD instance with the principal enabled")
			argoCD.Spec.ArgoCDAgent.Principal.LogLevel = "debug"
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			Eventually(paramsConfigMap, "90s", "5s").Should(k8sFixture.ExistByName())

			By("disabling the principal")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(argoCD), argoCD)).To(Succeed())
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.ArgoCDAgent.Principal.Enabled = new(false)
			})

			By("verifying the params ConfigMap is removed")
			Eventually(paramsConfigMap, "90s", "5s").Should(k8sFixture.NotExistByName())
		})
	})
})
