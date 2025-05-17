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
	"os"

	// "os"

	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	osFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/os"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-031_validate_toolchain", func() {

		var (
			k8sClient client.Client
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
		})

		// getPodName waits for there to exist a running pod with name 'name' in openshift-gitops
		getPodName := func(name string) (string, error) {

			var podName string

			if err := wait.PollUntilContextTimeout(context.Background(), time.Second*5, time.Minute*2, true, func(ctx context.Context) (done bool, err error) {

				var podList corev1.PodList
				if err := k8sClient.List(ctx, &podList, client.InNamespace("openshift-gitops")); err != nil {
					GinkgoWriter.Println(err)
					return false, nil
				}

				for _, pod := range podList.Items {
					if pod.Status.Phase == corev1.PodRunning {
						if strings.Contains(pod.Name, name) {
							podName = pod.Name
							return true, nil
						}
					}
				}
				return false, nil

			}); err != nil {
				return "", err
			}

			return podName, nil
		}

		It("verifies that toolchain versions have the expected values", func() {

			// These variables need to be maintained according to the component matrix: https://spaces.redhat.com/display/GITOPS/GitOps+Component+Matrix
			expected_kustomizeVersion := "v5.4.3"
			expected_helmVersion := "v3.16.3"
			expected_argocdVersion := "v2.14.4"

			var expected_dexVersion string
			var expected_redisVersion string

			if os.Getenv("CI") == "prow" {
				// when running against openshift-ci
				expected_dexVersion = "v2.30.3-dirty"
				expected_redisVersion = "6.2.4"

			} else {
				// when running against RC/ released version of gitops
				expected_dexVersion = "v2.35.1"
				expected_redisVersion = "6.2.7"
			}

			By("locating pods containing toolchain in openshift-gitops")

			gitops_server_pod, err := getPodName("openshift-gitops-server")
			Expect(err).ToNot(HaveOccurred())
			dex_pod, err := getPodName("openshift-gitops-dex-server")
			Expect(err).ToNot(HaveOccurred())
			redis_pod, err := getPodName("openshift-gitops-redis")
			Expect(err).ToNot(HaveOccurred())

			serverRoute := &routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-server", Namespace: "openshift-gitops"}}
			Eventually(serverRoute).Should(k8sFixture.ExistByName())

			By("extracting the kustomize version from container")

			kustomizeVersion, err := osFixture.ExecCommand("bash", "-c", "oc -n openshift-gitops exec "+gitops_server_pod+" -- kustomize version")

			Expect(err).NotTo(HaveOccurred())
			kustomizeVersion = strings.TrimSpace(kustomizeVersion)

			By("extracting the helm version from container")
			helmVersion, err := osFixture.ExecCommand("bash", "-c", "oc -n openshift-gitops exec "+gitops_server_pod+" -- helm version")
			Expect(err).NotTo(HaveOccurred())

			// output format:
			// version.BuildInfo{Version:"v3.15.4", GitCommit:"fa9efb07d9d8debbb4306d72af76a383895aa8c4", GitTreeState:"", GoVersion:"go1.22.9 (Red Hat 1.22.9-1.module+el8.10.0+22500+aee717ef)"
			helmVersion = helmVersion[strings.Index(helmVersion, "Version:"):]
			// After: Version:"v3.15.4" (...)
			helmVersion = helmVersion[strings.Index(helmVersion, "\"")+1:]
			// After: v3.15.4" (...)
			helmVersion = helmVersion[:strings.Index(helmVersion, "\"")]
			// After: v3.15.4

			By("extracting the argo cd server version from container")
			argocdVersion, err := osFixture.ExecCommand("bash", "-c", "oc -n openshift-gitops exec "+gitops_server_pod+" -- argocd version --short --server "+serverRoute.Spec.Host+" --insecure | grep 'argocd-server'")
			argocdVersion = strings.ReplaceAll(argocdVersion, "+unknown", "")
			// output format:
			// argocd-server: v2.13.1+af54ef8
			Expect(err).NotTo(HaveOccurred())

			By("extracting the dex version from container")
			dexVersionOutput, err := osFixture.ExecCommand("bash", "-c", "oc -n openshift-gitops exec "+dex_pod+" -- dex version")
			Expect(err).ToNot(HaveOccurred())
			// Output format:
			// Defaulted container "dex" out of: dex, copyutil (init)
			// Dex Version: v2.41.1-1-ga7854d65

			var dexVersion string
			dexVersionOutputSplit := strings.Split(dexVersionOutput, "\n")
			for _, line := range dexVersionOutputSplit {
				if strings.Contains(line, "Dex Version:") {
					dexVersion = line
					dexVersion = dexVersion[strings.Index(dexVersion, ":")+1:]
					// After: ' v2.41.1-1-ga7854d65'
					dexVersion = strings.TrimSpace(dexVersion)
					// After: 'v2.41.1-1-ga7854d65'
					break
				}
			}
			Expect(dexVersion).ToNot(BeEmpty())

			By("extracting the redis version from container")
			redisVersion, err := osFixture.ExecCommand("bash", "-c", "oc -n openshift-gitops exec "+redis_pod+" -- redis-server -v")
			// output format: Redis server v=6.2.7 sha=00000000:0 malloc=jemalloc-5.1.0 bits=64 build=5d88ce217879027a
			redisVersion = redisVersion[strings.Index(redisVersion, "v=")+2:]
			// After: v=6.2.7 (...)
			redisVersion = redisVersion[0:strings.Index(redisVersion, " ")]
			// After: v=6.2.7
			Expect(err).NotTo(HaveOccurred())

			By("verifying containers have expected toolchain versions")

			Expect(kustomizeVersion).To(Equal(expected_kustomizeVersion))
			Expect(helmVersion).To(Equal(expected_helmVersion))
			Expect(dexVersion).To(Equal(expected_dexVersion))

			// We are as argocdVersion contains v2.7.6+00c914a suffix addition to the version no.
			// So, we are checking if expected_argocdVersion is substring of the actual version
			Expect(argocdVersion).To(ContainSubstring(expected_argocdVersion))

			Expect(redisVersion).To(Equal(expected_redisVersion))

		})

	})
})
