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
	"strings"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-053_validate_cluster_admin_rbac", func() {

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
		})

		It("validates that openshift-gitops instance has expected .spec.RBAC.policy values", func() {

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops", Namespace: "openshift-gitops"},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			Expect(strings.TrimSpace(*argoCD.Spec.RBAC.Policy)).Should(Equal("g, system:cluster-admins, role:admin\ng, cluster-admins, role:admin"))

		})

	})

})
