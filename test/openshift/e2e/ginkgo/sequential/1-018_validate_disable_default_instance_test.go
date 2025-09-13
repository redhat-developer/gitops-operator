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

	"github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-018_validate_disable_default_instance", func() {

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
		})

		It("verifies that the default ArgoCD instance from openshift-gitops namespace is recreated when deleted manually", func() {

			openshiftGitopsArgoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			By("verifying that the openshift-gitops ArgoCD instance exists initially")
			Eventually(openshiftGitopsArgoCD).Should(k8sFixture.ExistByName())
			Eventually(openshiftGitopsArgoCD).Should(argocdFixture.BeAvailable())

			By("verifying associated deployments exist")
			deploymentsToVerify := []string{
				"openshift-gitops-server",
				"openshift-gitops-redis",
				"openshift-gitops-repo-server",
				"openshift-gitops-applicationset-controller",
			}

			for _, deplName := range deploymentsToVerify {
				depl := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: deplName, Namespace: "openshift-gitops"},
				}
				Eventually(depl).Should(k8sFixture.ExistByName())
			}

			ss := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-application-controller",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(ss).Should(k8sFixture.ExistByName())

			By("manually deleting the openshift-gitops ArgoCD instance")
			k8sClient, _ := utils.GetE2ETestKubeClient()
			err = k8sClient.Delete(context.Background(), openshiftGitopsArgoCD)
			Expect(err).ToNot(HaveOccurred())

			By("verifying ArgoCD CR gets recreated automatically by the operator")
			openshiftGitopsArgoCD = &v1beta1.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(openshiftGitopsArgoCD, "3m", "5s").Should(k8sFixture.ExistByName())
			Eventually(openshiftGitopsArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying deployments and statefulset are recreated and become ready")
			for _, deplName := range deploymentsToVerify {
				depl := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: deplName, Namespace: "openshift-gitops"},
				}
				Eventually(depl, "3m", "5s").Should(k8sFixture.ExistByName())
				Eventually(depl, "5m", "5s").Should(deployment.HaveReadyReplicas(1))
			}

			Eventually(ss, "3m", "5s").Should(k8sFixture.ExistByName())
			Eventually(ss, "5m", "5s").Should(statefulsetFixture.HaveReadyReplicas(1))
		})

		It("verifies that DISABLE_DEFAULT_ARGOCD_INSTANCE env var will delete the argo cd instance from openshift-gitops, and that default Argo CD instance will be restored when the env var is removed", func() {
			if fixture.EnvLocalRun() {
				Skip("when running locally, there is no subscription or operator deployment to modify, so this test is skipped.")
				return
			}
			openshiftGitopsArgoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())

			Eventually(openshiftGitopsArgoCD).Should(k8sFixture.ExistByName())

			By("disabling default Argo CD instance via env var")

			fixture.SetEnvInOperatorSubscriptionOrDeployment("DISABLE_DEFAULT_ARGOCD_INSTANCE", "true")

			By("verifying operator restarts with DISABLE_DEFAULT_ARGOCD_INSTANCE set")
			operatorControllerDepl := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-operator-controller-manager",
					Namespace: "openshift-gitops-operator", // The original kuttl test was 'openshift-operators'
				},
			}
			Eventually(operatorControllerDepl).Should(k8sFixture.ExistByName())
			Eventually(operatorControllerDepl).Should(deployment.HaveContainerWithEnvVar("DISABLE_DEFAULT_ARGOCD_INSTANCE", "true", 0))
			Eventually(operatorControllerDepl).Should(deployment.HaveReplicas(1))
			Eventually(operatorControllerDepl).Should(deployment.HaveAvailableReplicas(1))
			Eventually(operatorControllerDepl).Should(deployment.HaveReadyReplicas(1))

			By("verifying ArgoCD CR no longer exists")
			openshiftGitopsArgoCD = &v1beta1.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(openshiftGitopsArgoCD).Should(k8sFixture.NotExistByName())

			By("verifying Argo CD gitops service Deployment no longer exists")
			gitopsServerDepl := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-server",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(gitopsServerDepl).Should(k8sFixture.NotExistByName())

			By("remove the DISABLE_DEFAULT_ARGOCD_INSTANCE env var we set above")
			fixture.RestoreSubcriptionToDefault()

			Eventually(openshiftGitopsArgoCD, "3m", "5s").Should(k8sFixture.ExistByName())
			Eventually(openshiftGitopsArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			By("verifying deployment and statefulset have expected number of replicas, including the repo server which should have 2")
			deploymentsToVerify := []string{
				"openshift-gitops-server",
				"openshift-gitops-redis",
				"openshift-gitops-repo-server",
				"openshift-gitops-applicationset-controller",
			}

			for _, deplToVerify := range deploymentsToVerify {

				depl := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: deplToVerify, Namespace: openshiftGitopsArgoCD.Namespace},
				}
				Eventually(depl).Should(k8sFixture.ExistByName())

				Eventually(depl).Should(deployment.HaveReplicas(1))
				Eventually(depl, "2m", "5s").Should(deployment.HaveReadyReplicas(1))
			}

			ss := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-application-controller",
					Namespace: openshiftGitopsArgoCD.Namespace,
				},
			}
			Eventually(ss).Should(k8sFixture.ExistByName())
			Eventually(ss).Should(statefulsetFixture.HaveReplicas(1))
			Eventually(ss, "2m", "5s").Should(statefulsetFixture.HaveReadyReplicas(1))

		})

	})
})
