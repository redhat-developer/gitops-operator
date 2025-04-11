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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-063_validate_dex_liveness_probe_test", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies dex server Pod has expected liveness probe values", func() {

			By("verifying Argo CD is ready")
			argoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying dex server Pod has expected liveness probe values")
			Eventually(func() bool {

				var podList corev1.PodList
				if err := k8sClient.List(ctx, &podList, &client.ListOptions{Namespace: "openshift-gitops"}); err != nil {
					GinkgoWriter.Println(err)
					return false
				}

				var pod corev1.Pod
				for idx := range podList.Items {
					currPod := podList.Items[idx]
					if val, exists := currPod.Labels["app.kubernetes.io/name"]; exists && val == "openshift-gitops-dex-server" {
						pod = currPod
						break
					}
				}

				if len(pod.Spec.Containers) != 1 {
					return false
				}

				container := pod.Spec.Containers[0]
				livenessProbe := container.LivenessProbe
				if livenessProbe == nil {
					return false
				}

				if livenessProbe.FailureThreshold != int32(3) {
					return false
				}

				httpGet := livenessProbe.HTTPGet
				if (*httpGet).Path != "/healthz/live" {
					return false
				}

				if (*httpGet).Port != intstr.FromInt(5558) {
					return false
				}
				if (*httpGet).Scheme != corev1.URISchemeHTTP {
					return false
				}
				if livenessProbe.InitialDelaySeconds != int32(60) {
					return false
				}

				if livenessProbe.PeriodSeconds != int32(30) {
					return false
				}

				if livenessProbe.SuccessThreshold != int32(1) {
					return false
				}

				if livenessProbe.TimeoutSeconds != int32(1) {
					return false
				}

				return true

			}).Should(BeTrue())

		})

	})
})
