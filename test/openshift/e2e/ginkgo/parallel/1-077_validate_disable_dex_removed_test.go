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
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-077_validate_disable_dex_removed", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that DISABLE_DEX is not specified in the Subscription/CSV that was used to install the operator", func() {

			if fixture.EnvNonOLM() || fixture.EnvLocalRun() {
				Skip("this test requires operator to have been installed via OLM")
				return
			}

			var operatorNameVersion string

			By("getting the Subscription and CSV that was used to install the operator")

			if fixture.EnvCI() {
				subscription, err := fixture.GetSubscriptionInEnvCIEnvironment(k8sClient)
				Expect(err).ToNot(HaveOccurred())
				Expect(subscription).ToNot(BeNil())

				operatorNameVersion = subscription.Status.InstalledCSV

			} else {
				subscription := &olmv1alpha1.Subscription{
					ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator", Namespace: "openshift-gitops-operator"},
				}
				Expect(subscription).Should(k8sFixture.ExistByName())

				operatorNameVersion = subscription.Status.InstalledCSV

			}

			Expect(operatorNameVersion).ShouldNot(BeEmpty())

			By("verifying that the CSV install spec does not contain DISABLE_DEX")

			csv := &olmv1alpha1.ClusterServiceVersion{ObjectMeta: metav1.ObjectMeta{
				Name:      operatorNameVersion,
				Namespace: "openshift-gitops-operator",
			}}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(csv), csv)).To(Succeed())

			deploymentSpecs := csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs
			outBytes, err := json.Marshal(deploymentSpecs)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(outBytes)).ToNot(ContainSubstring("DISABLE_DEX"), "DISABLE_DEX should not be present in the operator CSV.")

		})

	})
})
