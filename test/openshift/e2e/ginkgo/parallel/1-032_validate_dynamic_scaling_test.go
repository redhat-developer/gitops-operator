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

package parallel

import (
	"context"
	"fmt"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-032_validate_dynamic_scaling", func() {

		var (
			k8sClient   client.Client
			ctx         context.Context
			ns          *corev1.Namespace
			cleanupFunc func()
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()

		})

		AfterEach(func() {

			fixture.OutputDebugOnFail(ns)
			if cleanupFunc != nil {
				cleanupFunc()
			}
		})

		It("ensures that when dynamic scaling is enabled, that Argo CD application controller StatefulSet will scale up/down based on number of Secrets", func() {

			By("creating namespace-scoped Argo CD instance with dynamic scaling enabled and a min/max number of shards, and 1 shard per cluster")
			ns, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Controller: argov1beta1api.ArgoCDApplicationControllerSpec{
						Sharding: argov1beta1api.ArgoCDApplicationControllerShardSpec{
							DynamicScalingEnabled: ptr.To(true),
							MinShards:             1,
							MaxShards:             4,
							ClustersPerShard:      1,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("create 2 cluster secrets")
			createSecret := func(nameSuffix string) *corev1.Secret {
				res := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-" + nameSuffix,
						Namespace: ns.Name,
						Labels: map[string]string{
							"argocd.argoproj.io/secret-type": "cluster",
						},
					},
					StringData: map[string]string{
						"name":   "mycluster-" + nameSuffix + ".com",
						"server": "https://mycluster-" + nameSuffix + ".com",
					},
					Type: corev1.SecretTypeOpaque,
				}
				Expect(k8sClient.Create(ctx, res)).To(Succeed())
				return res
			}

			createSecret("1")
			createSecret("2")

			By("verifying that application controller replicas increases to 3, based on the Secrets created")

			appControllerSS := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-application-controller",
					Namespace: ns.Name,
				},
			}
			Eventually(appControllerSS, "90s", "5s").Should(statefulsetFixture.HaveReplicas(3))

			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("creating 4 more secrets")

			createSecret("3")
			createSecret("4")
			createSecret("5")
			createSecret("6")

			By("verifying that application controller replicas increases to 4, based on the Secrets created")

			Eventually(appControllerSS, "90s", "5s").Should(statefulsetFixture.HaveReplicas(4))

			By("waiting for ArgoCD CR to be reconciled and available")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("deleting all the secrets except for 1")
			for x := 2; x <= 6; x++ {

				secretToDelete := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("cluster-%d", x),
						Namespace: ns.Name,
					},
				}
				Expect(k8sClient.Delete(ctx, secretToDelete)).To(Succeed())

			}

			By("verifying that application controller replicas decreases to 2")
			Eventually(appControllerSS, "90s", "5s").Should(statefulsetFixture.HaveReplicas(2))

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

		})
	})
})
