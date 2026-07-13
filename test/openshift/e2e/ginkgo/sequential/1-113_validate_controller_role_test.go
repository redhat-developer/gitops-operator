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

Migrated from operator-e2e kuttl: gitops-operator/tests/sequential/1-113_validate_controller_role
*/

package sequential

import (
	"context"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var aggregatedControllerRoleRule = rbacv1.PolicyRule{
	Verbs:     []string{"create", "update", "patch", "delete"},
	APIGroups: []string{"test.com"},
	Resources: []string{"test"},
}

func roleContainsPolicyRule(k8sClient client.Client, role *rbacv1.Role, expected rbacv1.PolicyRule) bool {
	if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(role), role); err != nil {
		GinkgoWriter.Println(err)
		return false
	}

	for _, rule := range role.Rules {
		if reflect.DeepEqual(rule, expected) {
			return true
		}
	}

	return false
}

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-113_validate_controller_role", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
			testNS    *corev1.Namespace
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		AfterEach(func() {
			fixture.OutputDebugOnFail(testNS, "openshift-gitops")
		})

		It("validates openshift-gitops application-controller Role aggregates admin ClusterRole rules and removes them on delete", func() {

			By("creating a namespace managed by openshift-gitops")
			testNS = fixture.CreateManagedNamespace("test-1-113-ns", "openshift-gitops")
			defer func() {
				Expect(k8sClient.Delete(ctx, testNS)).To(Succeed())
			}()

			openshiftGitopsArgoCD, err := argocdFixture.GetOpenShiftGitOpsNSArgoCD()
			Expect(err).ToNot(HaveOccurred())
			Eventually(openshiftGitopsArgoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			appControllerRole := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openshift-gitops-argocd-application-controller",
					Namespace: testNS.Name,
				},
			}

			By("verifying openshift-gitops application-controller Role is created in the managed namespace")
			Eventually(appControllerRole).Should(k8sFixture.ExistByName())

			aggregateClusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-1-113-sample",
					Labels: map[string]string{
						"rbac.authorization.k8s.io/aggregate-to-admin": "true",
					},
				},
				Rules: []rbacv1.PolicyRule{aggregatedControllerRoleRule},
			}

			By("creating a ClusterRole with aggregate-to-admin label")
			Expect(k8sClient.Create(ctx, aggregateClusterRole)).To(Succeed())
			defer func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, aggregateClusterRole))).To(Succeed())
				Eventually(aggregateClusterRole).Should(k8sFixture.NotExistByName())
			}()

			By("verifying aggregated rules are added to the application-controller Role")
			Eventually(func() bool {
				return roleContainsPolicyRule(k8sClient, appControllerRole, aggregatedControllerRoleRule)
			}, "3m", "5s").Should(BeTrue())

			By("deleting the aggregate ClusterRole")
			Expect(k8sClient.Delete(ctx, aggregateClusterRole)).To(Succeed())
			Eventually(aggregateClusterRole).Should(k8sFixture.NotExistByName())

			By("verifying aggregated rules are removed from the application-controller Role")
			Eventually(func() bool {
				return roleContainsPolicyRule(k8sClient, appControllerRole, aggregatedControllerRoleRule)
			}, "3m", "5s").Should(BeFalse())
			Consistently(func() bool {
				return roleContainsPolicyRule(k8sClient, appControllerRole, aggregatedControllerRoleRule)
			}, "30s", "5s").Should(BeFalse())
		})
	})
})
