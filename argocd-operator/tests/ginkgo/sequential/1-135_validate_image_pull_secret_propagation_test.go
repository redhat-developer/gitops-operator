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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	"github.com/argoproj-labs/argocd-operator/common"
	"github.com/argoproj-labs/argocd-operator/tests/ginkgo/fixture"
	argocdFixture "github.com/argoproj-labs/argocd-operator/tests/ginkgo/fixture/argocd"
	fixtureUtils "github.com/argoproj-labs/argocd-operator/tests/ginkgo/fixture/utils"
)

// The e2e operator runs as a local process (make start-e2e), so it has no operator
// namespace and reconcileImagePullSecrets skips the operator-NS->instance-NS copy.
// The in-namespace path is what is exercised here: a Secret labeled
// propagate-image-pull-secret=true in the instance namespace is resolved by
// getImagePullSecretRefs and set as imagePullSecrets on the component ServiceAccounts.
var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-142_validate_image_pull_secret_propagation", func() {

		var (
			k8sClient   client.Client
			ctx         context.Context
			ns          *corev1.Namespace
			cleanupFunc func()
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			var err error
			k8sClient, _, err = fixtureUtils.GetE2ETestKubeClientWithError()
			Expect(err).NotTo(HaveOccurred())
			ctx = context.Background()

			ns, cleanupFunc = fixture.CreateNamespaceWithCleanupFunc("argocd")
		})

		AfterEach(func() {
			if cleanupFunc != nil {
				cleanupFunc()
			}
		})

		// dockerCfg is a minimal valid .dockerconfigjson payload.
		dockerCfg := map[string][]byte{".dockerconfigjson": []byte(`{"auths":{}}`)}

		newLabeledPullSecret := func(name, ns string) *corev1.Secret {
			return &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ns,
					Labels:    map[string]string{common.ArgoCDImagePullSecretPropagateLabel: "true"},
				},
				Type: corev1.SecretTypeDockerConfigJson,
				Data: dockerCfg,
			}
		}

		// expectSAPullSecret asserts (eventually) whether the named ServiceAccount's
		// imagePullSecrets contains secretName. There is no ServiceAccount fixture matcher.
		expectSAPullSecret := func(saName, ns, secretName string, present bool) {
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: saName, Namespace: ns}, sa)).To(Succeed())
				names := make([]string, 0, len(sa.ImagePullSecrets))
				for _, r := range sa.ImagePullSecrets {
					names = append(names, r.Name)
				}
				if present {
					g.Expect(names).To(ContainElement(secretName))
				} else {
					g.Expect(names).NotTo(ContainElement(secretName))
				}
			}, "2m", "3s").Should(Succeed())
		}

		It("sets imagePullSecrets on component ServiceAccounts from an in-namespace labeled Secret", func() {

			By("creating a labeled pull Secret before the ArgoCD instance")
			Expect(k8sClient.Create(ctx, newLabeledPullSecret("my-pull-secret", ns.Name))).To(Succeed())

			By("creating the ArgoCD instance")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: ns.Name},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying server and application-controller ServiceAccounts carry the pull secret")
			serverSA := "example-argocd-" + common.ArgoCDServerComponent
			appCtrlSA := "example-argocd-" + common.ArgoCDApplicationControllerComponent
			expectSAPullSecret(serverSA, ns.Name, "my-pull-secret", true)
			expectSAPullSecret(appCtrlSA, ns.Name, "my-pull-secret", true)

			By("verifying the reference is stable")
			Consistently(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serverSA, Namespace: ns.Name}, sa)).To(Succeed())
				names := make([]string, 0, len(sa.ImagePullSecrets))
				for _, r := range sa.ImagePullSecrets {
					names = append(names, r.Name)
				}
				g.Expect(names).To(ContainElement("my-pull-secret"))
			}, "30s", "5s").Should(Succeed())
		})

		It("skips propagation when multiple labeled pull Secrets exist in the namespace", func() {

			By("creating two labeled pull Secrets before the ArgoCD instance")
			Expect(k8sClient.Create(ctx, newLabeledPullSecret("pull-secret-1", ns.Name))).To(Succeed())
			Expect(k8sClient.Create(ctx, newLabeledPullSecret("pull-secret-2", ns.Name))).To(Succeed())

			By("creating the ArgoCD instance")
			argocdNS, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: argocdNS.Name},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying the server ServiceAccount carries neither pull secret")
			serverSA := "example-argocd-" + common.ArgoCDServerComponent
			Consistently(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serverSA, Namespace: argocdNS.Name}, sa)).To(Succeed())
				names := make([]string, 0, len(sa.ImagePullSecrets))
				for _, r := range sa.ImagePullSecrets {
					names = append(names, r.Name)
				}
				g.Expect(names).NotTo(ContainElement("pull-secret-1"))
				g.Expect(names).NotTo(ContainElement("pull-secret-2"))
			}, "1m", "5s").Should(Succeed())
		})

		It("removes imagePullSecrets from ServiceAccounts when the labeled Secret is deleted", func() {

			By("creating a labeled pull Secret before the ArgoCD instance")
			pullSecret := newLabeledPullSecret("my-pull-secret", ns.Name)
			Expect(k8sClient.Create(ctx, pullSecret)).To(Succeed())

			By("creating the ArgoCD instance")
			argocdNS, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: argocdNS.Name},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			serverSA := "example-argocd-" + common.ArgoCDServerComponent
			expectSAPullSecret(serverSA, argocdNS.Name, "my-pull-secret", true)

			By("deleting the labeled pull Secret")
			Expect(k8sClient.Delete(ctx, pullSecret)).To(Succeed())

			By("verifying the pull secret is removed from the server ServiceAccount")
			expectSAPullSecret(serverSA, argocdNS.Name, "my-pull-secret", false)
		})

		It("propagates the pull secret to all component ServiceAccounts and removes it when the label is set to false", func() {

			By("creating a labeled pull Secret before the ArgoCD instance")
			pullSecret := newLabeledPullSecret("all-component-pull-secret", ns.Name)
			Expect(k8sClient.Create(ctx, pullSecret)).To(Succeed())

			By("creating the ArgoCD instance a new namespace")
			argocdNS, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: argocdNS.Name},
			}
			argoCD.Spec = argov1beta1api.ArgoCDSpec{
				Controller: argov1beta1api.ArgoCDApplicationControllerSpec{
					Enabled: new(true),
				},
				Redis: argov1beta1api.ArgoCDRedisSpec{
					Enabled: new(true),
				},
				Repo: argov1beta1api.ArgoCDRepoSpec{
					Enabled: new(true),
				},
				Server: argov1beta1api.ArgoCDServerSpec{
					Enabled: new(true),
				},
				ApplicationSet: &argov1beta1api.ArgoCDApplicationSet{
					Enabled: new(true),
				},
				Notifications: argov1beta1api.ArgoCDNotifications{
					Enabled: true,
				},
				Promoter: &argov1beta1api.PromoterSpec{
					Enabled: new(true),
				},
				ImageUpdater: argov1beta1api.ArgoCDImageUpdaterSpec{
					Enabled: true,
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			//list all service accounts in the namespace
			serviceAccounts := &corev1.ServiceAccountList{}
			Expect(k8sClient.List(ctx, serviceAccounts, client.InNamespace(argocdNS.Name))).To(Succeed())
			for _, sa := range serviceAccounts.Items {
				if sa.Name == "default" {
					continue
				}
				By("verifying the " + sa.Name + " ServiceAccount carries the pull secret")
				expectSAPullSecret(sa.Name, argocdNS.Name, "all-component-pull-secret", true)
			}

			By("labeling the pull Secret with propagate-image-pull-secret to false")
			pullSecret.Labels[common.ArgoCDImagePullSecretPropagateLabel] = "false"
			Expect(k8sClient.Update(ctx, pullSecret)).To(Succeed())

			for _, sa := range serviceAccounts.Items {
				if sa.Name == "default" {
					continue
				}
				By("verifying the " + sa.Name + " ServiceAccount does not carry the pull secret")
				expectSAPullSecret(sa.Name, argocdNS.Name, "all-component-pull-secret", false)
			}
		})
		It("sets imagePullSecrets on agent principal ServiceAccount", func() {

			By("creating a labeled pull Secret in the operator namespace")
			Expect(k8sClient.Create(ctx, newLabeledPullSecret("my-pull-secret", ns.Name))).To(Succeed())

			By("creating the ArgoCD instance in a separate namespace")
			argocdNS, argocdNSCleanup := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer argocdNSCleanup()
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: argocdNS.Name},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("enabling agent principal")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: argoCD.Name, Namespace: argocdNS.Name}, argoCD)).To(Succeed())
			argoCD.Spec.ArgoCDAgent = &argov1beta1api.ArgoCDAgentSpec{
				Principal: &argov1beta1api.PrincipalSpec{
					Enabled: new(true),
				},
			}
			Expect(k8sClient.Update(ctx, argoCD)).To(Succeed())

			By("verifying the agent principal ServiceAccount carries the copied pull secret")
			principalSA := argoCD.Name + "-agent-principal"
			expectSAPullSecret(principalSA, argocdNS.Name, "my-pull-secret", true)
		})

		It("sets imagePullSecrets on agent ServiceAccount", func() {

			By("creating a labeled pull Secret in the operator namespace")
			Expect(k8sClient.Create(ctx, newLabeledPullSecret("my-pull-secret", ns.Name))).To(Succeed())

			By("creating the ArgoCD instance in a separate namespace")
			argocdNS, argocdNSCleanup := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer argocdNSCleanup()
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: argocdNS.Name},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("enabling agent")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: argoCD.Name, Namespace: argocdNS.Name}, argoCD)).To(Succeed())
			argoCD.Spec.ArgoCDAgent = &argov1beta1api.ArgoCDAgentSpec{
				Agent: &argov1beta1api.AgentSpec{
					Enabled: new(true),
				},
			}
			Expect(k8sClient.Update(ctx, argoCD)).To(Succeed())

			By("verifying the agent ServiceAccount carries the copied pull secret")
			agentSA := argoCD.Name + "-agent-agent"
			expectSAPullSecret(agentSA, argocdNS.Name, "my-pull-secret", true)
		})

		It("sets imagePullSecrets on Redis HA ServiceAccount", func() {

			By("creating a labeled pull Secret in the operator namespace")
			Expect(k8sClient.Create(ctx, newLabeledPullSecret("my-pull-secret", ns.Name))).To(Succeed())

			By("creating the ArgoCD instance with HA enabled in a separate namespace")
			argocdNS, argocdNSCleanup := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer argocdNSCleanup()
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: argocdNS.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					HA: argov1beta1api.ArgoCDHASpec{Enabled: true},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying the redis-ha ServiceAccount carries the copied pull secret")
			redisSA := "example-argocd-" + common.ArgoCDRedisHAComponent
			expectSAPullSecret(redisSA, argocdNS.Name, "my-pull-secret", true)
		})

		It("sets imagePullSecrets on Dex ServiceAccount", func() {

			By("creating a labeled pull Secret in the operator namespace")
			Expect(k8sClient.Create(ctx, newLabeledPullSecret("my-pull-secret", ns.Name))).To(Succeed())

			By("creating the ArgoCD instance with Dex SSO enabled in a separate namespace")
			argocdNS, argocdNSCleanup := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer argocdNSCleanup()
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: argocdNS.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					SSO: &argov1beta1api.ArgoCDSSOSpec{
						Provider: argov1beta1api.SSOProviderTypeDex,
						Dex: &argov1beta1api.ArgoCDDexSpec{
							Config: "connectors: []",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("verifying the dex-server ServiceAccount carries the copied pull secret")
			dexSA := "example-argocd-" + common.ArgoCDDexServerComponent
			expectSAPullSecret(dexSA, argocdNS.Name, "my-pull-secret", true)
		})

		It("should propagate if pullsecret is created after the ArgoCD instance", func() {

			By("creating the ArgoCD instance with Dex SSO enabled in a separate namespace")
			argocdNS, argocdNSCleanup := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer argocdNSCleanup()
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "example-argocd", Namespace: argocdNS.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					SSO: &argov1beta1api.ArgoCDSSOSpec{
						Provider: argov1beta1api.SSOProviderTypeDex,
						Dex: &argov1beta1api.ArgoCDDexSpec{
							Config: "connectors: []",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("creating a labeled pull Secret in the operator namespace")
			Expect(k8sClient.Create(ctx, newLabeledPullSecret("my-pull-secret", ns.Name))).To(Succeed())

			By("verifying the dex-server ServiceAccount carries the copied pull secret")
			dexSA := "example-argocd-" + common.ArgoCDDexServerComponent
			expectSAPullSecret(dexSA, argocdNS.Name, "my-pull-secret", true)
		})
	})
})
