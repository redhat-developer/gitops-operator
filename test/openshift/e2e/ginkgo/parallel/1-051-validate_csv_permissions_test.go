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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	osFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/os"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-051-validate_csv_permissions", func() {

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

		})

		It("verifies operator can delete resourcequotas", func() {

			if fixture.EnvNonOLM() {
				Skip("Skipping test as NON_OLM env var is set. This test requires openshift-gitops operator to be installed via OLM")
				return
			}

			if fixture.EnvLocalRun() {
				Skip("Skipping test as LOCAL_RUN env var is set. The operator service account does not exist in this case")
				return
			}

			By("run oc command to verify our ability to delete resourcequotas")
			res, err := osFixture.ExecCommand("oc", "auth", "can-i", "delete", "resourcequotas", "-n", "openshift-gitops", "--as", "system:serviceaccount:openshift-gitops-operator:openshift-gitops-operator-controller-manager")
			Expect(err).ToNot(HaveOccurred())
			Expect(strings.TrimSpace(res)).To(Equal("yes"))

		})

	})
})
