/*
Copyright 2021.

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

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	securityv1 "github.com/openshift/api/security/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	nodeFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/node"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/pod"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-071_validate_SCC_HA", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("creates SCC and ensure HA Argo CD starts as expected", func() {

			By("verifying we are running on a cluster with at least 3 nodes. This is required for Redis HA")
			nodeFixture.ExpectHasAtLeastXNodes(3)

			scc := &securityv1.SecurityContextConstraints{
				ObjectMeta: metav1.ObjectMeta{
					Name: "restricted-dropcaps",
				},
			}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(scc), scc); err == nil {
				Expect(k8sClient.Delete(ctx, scc)).To(Succeed())
			} else {
				Expect(err).ToNot(BeNil())
			}

			scc = &securityv1.SecurityContextConstraints{
				ObjectMeta: metav1.ObjectMeta{
					Name: "restricted-dropcaps",
				},
				AllowHostDirVolumePlugin: false,
				AllowHostIPC:             false,
				AllowHostNetwork:         false,
				AllowHostPID:             false,
				AllowHostPorts:           false,
				AllowPrivilegeEscalation: ptr.To(false),
				AllowPrivilegedContainer: false,
				AllowedCapabilities:      nil,
				DefaultAddCapabilities:   nil,
				FSGroup: securityv1.FSGroupStrategyOptions{
					Type: securityv1.FSGroupStrategyMustRunAs,
				},
				Groups:                 []string{"system:authenticated"},
				Priority:               nil,
				ReadOnlyRootFilesystem: false,
				RequiredDropCapabilities: []corev1.Capability{
					"KILL",
					"MKNOD",
					"SETUID",
					"SETGID",
					"CHOWN",
					"DAC_OVERRIDE",
					"FOWNER",
					"FSETID",
					"SETPCAP",
					"NET_BIND_SERVICE",
				},
				RunAsUser: securityv1.RunAsUserStrategyOptions{
					Type: securityv1.RunAsUserStrategyMustRunAsRange,
				},
				SELinuxContext: securityv1.SELinuxContextStrategyOptions{
					Type: securityv1.SELinuxStrategyMustRunAs,
				},
				SupplementalGroups: securityv1.SupplementalGroupsStrategyOptions{
					Type: securityv1.SupplementalGroupsStrategyRunAsAny,
				},
				Users: []string{},
				Volumes: []securityv1.FSType{
					"configMap",
					"downwardAPI",
					"emptyDir",
					"persistentVolumeClaim",
					"projected",
					"secret",
				},
			}
			Expect(k8sClient.Create(ctx, scc)).To(Succeed())

			defer func() {
				Expect(k8sClient.Delete(ctx, scc)).To(Succeed())
			}()

			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			By("creating simple namespace-scoped Argo CD instance")
			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: ns.Name},
				Spec: argov1beta1api.ArgoCDSpec{
					Server: argov1beta1api.ArgoCDServerSpec{
						Route: argov1beta1api.ArgoCDRouteSpec{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			expectPodsLabelsAreFoundAndRunningByPodLabel := func(expectedPodLabels []string) {

				By("verifying workload pods exist and are running")
				var pods corev1.PodList
				Expect(k8sClient.List(ctx, &pods, &client.ListOptions{Namespace: ns.Name})).To(Succeed())

				for _, expectedPodLabel := range expectedPodLabels {
					match := false
					for _, pod := range pods.Items {
						if pod.Labels != nil && pod.Labels["app.kubernetes.io/name"] == expectedPodLabel {
							match = true
							Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))
							break
						}
					}
					Expect(match).To(BeTrue(), "unable to locate Pod with label 'app.kubernetes.io/name' of "+expectedPodLabel)
				}

			}

			expectedPodLabels := []string{"argocd-application-controller", "argocd-redis", "argocd-repo-server", "argocd-server"}
			expectPodsLabelsAreFoundAndRunningByPodLabel(expectedPodLabels)

			By("enabling HA on Argo CD")
			argocdFixture.Update(argoCD, func(ac *argov1beta1api.ArgoCD) {
				ac.Spec.HA.Enabled = true
			})

			By("waiting for HA to be enabled on Argo CD, and Argo CD to be ready")
			Eventually(argoCD, "5m", "10s").Should(argocdFixture.BeAvailable()) // enabling HA takes a while

			expectedPodLabels = []string{"argocd-application-controller", "argocd-redis-ha-haproxy", "argocd-repo-server", "argocd-server"}
			expectPodsLabelsAreFoundAndRunningByPodLabel(expectedPodLabels)

			By("verifying workload HA pods exist and are running")
			var pods corev1.PodList
			Expect(k8sClient.List(ctx, &pods, &client.ListOptions{Namespace: ns.Name})).To(Succeed())

			redisServer1Pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "argocd-redis-ha-server-1", Namespace: ns.Name}}
			Eventually(redisServer1Pod, "3m", "1s").Should(k8sFixture.ExistByName())
			Eventually(redisServer1Pod, "3m", "1s").Should(pod.HavePhase(corev1.PodRunning))

			redisServer2Pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "argocd-redis-ha-server-2", Namespace: ns.Name}}
			Eventually(redisServer2Pod, "3m", "1s").Should(k8sFixture.ExistByName())
			Eventually(redisServer2Pod, "3m", "1s").Should(pod.HavePhase(corev1.PodRunning))

		})

	})
})
