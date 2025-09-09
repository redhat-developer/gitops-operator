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
	"os"
	"path"
	"path/filepath"
	"strings"

	osFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/os"
	"gopkg.in/yaml.v3"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// default used when E2E_MUST_GATHER_IMAGE is not set.
// CI images:
// - quay.io/redhat-user-workloads/rh-openshift-gitops-tenant/gitops-must-gather:on-pr-<GIT_COMMIT_SHA>
// - quay.io/redhat-user-workloads/rh-openshift-gitops-tenant/gitops-must-gather:<GIT_COMMIT_SHA>
// - quay.io/redhat-user-workloads/rh-openshift-gitops-tenant/gitops-must-gather:latest # For main branch.
const defaultMustGatherImage = "quay.io/redhat-user-workloads/rh-openshift-gitops-tenant/gitops-must-gather:latest"

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-120_validate_running_must_gather", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verified the files collected for must gather are valid", func() {
			By("creating namespace-scoped Argo CD instance")
			ns, nsCleanup := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer nsCleanup()

			nsf, nsfCleanup := fixture.CreateManagedNamespaceWithCleanupFunc(ns.Name+"-follower", ns.Name)
			defer nsfCleanup()

			argoCD := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: "left-argocd", Namespace: ns.Name},
				Spec:       argov1beta1api.ArgoCDSpec{},
			}
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CRs to be reconciled and the instances to be ready in " + ns.Name)
			Eventually(argoCD, "5m", "5s").Should(argocdFixture.BeAvailable())

			// TODO https://github.com/redhat-developer/gitops-must-gather/blob/135850b74b56b6fda9fc68ed4165a88b5c7dbeaf/gather_gitops.sh#L40
			// TODO https://github.com/redhat-developer/gitops-must-gather/blob/135850b74b56b6fda9fc68ed4165a88b5c7dbeaf/gather_gitops.sh#L61
			// TODO https://github.com/redhat-developer/gitops-must-gather/blob/135850b74b56b6fda9fc68ed4165a88b5c7dbeaf/gather_gitops.sh#L79

			destDir := gather()
			defer os.RemoveAll(destDir)

			// TODO: Not before 4.16: https://github.com/openshift/oc/commit/7d23cbb68dfed274b2821d91038f45c8ce12a249
			// Expect(path.Join(destDir, "must-gather.logs")).To(BeARegularFile())

			Expect(path.Join(destDir, "event-filter.html")).To(BeARegularFile())
			Expect(path.Join(destDir, "timestamp")).To(BeARegularFile())

			resources := resourcesDir(destDir)
			// TODO: Not before 4.16: https://github.com/openshift/oc/commit/6348e4a0484fce9b4151dbf39ca17bdd8a450053
			// Expect(path.Join(resources, "gather.logs")).To(BeARegularFile())
			csr := path.Join(resources, "cluster-scoped-resources")
			Expect(csr).To(BeADirectory())
			Expect(path.Join(csr, "apiextensions.k8s.io/customresourcedefinitions/applications.argoproj.io.yaml")).To(BeValidResourceFile())
			Expect(path.Join(csr, "argoproj.io/clusteranalysistemplates.yaml")).To(BeValidResourceFile())
			Expect(path.Join(csr, "config.openshift.io/clusterversions/version.yaml")).To(BeValidResourceFile())

			n := path.Join(resources, "namespaces")
			Expect(n).To(BeADirectory())
			Expect(path.Join(n, "openshift-gitops/openshift-gitops.yaml")).To(BeValidResourceFile())
			Expect(path.Join(n, "openshift-gitops/route.openshift.io/routes.yaml")).To(BeValidResourceFile())
			Expect(path.Join(n, "openshift-gitops/argoproj.io/appprojects.yaml")).To(BeValidResourceFile())
			logs := path.Join(n, "openshift-gitops/pods/openshift-gitops-application-controller-0/argocd-application-controller/argocd-application-controller/logs/")
			Expect(path.Join(logs, "current.log")).To(BeARegularFile())
			Expect(path.Join(logs, "previous.log")).To(BeARegularFile())

			Expect(path.Join(n, nsf.Name, "core/pods.yaml")).To(BeValidResourceFile())
		})
	})
})

func gather() string {
	destDir, err := os.MkdirTemp("", "gitops-operator-e2e-must-gather-test-1-120_*")
	Expect(err).ToNot(HaveOccurred())

	stdout, err := osFixture.ExecCommandWithOutputParam(
		true,
		"oc", "adm", "must-gather", "--image", mustGatherImage(), "--dest-dir", destDir,
	)
	Expect(err).ToNot(HaveOccurred())

	errorLines := make([]string, 0)
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, "error:") {
			errorLines = append(errorLines, line)
		}
	}
	Expect(errorLines).To(BeEmpty(), "Errors found in must gather output")
	return destDir
}

func resourcesDir(destDir string) string {
	// Find the only subdirectory which contains must-gather data
	entries, err := os.ReadDir(destDir)
	Expect(err).ToNot(HaveOccurred())

	var subdirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			subdirs = append(subdirs, entry.Name())
		}
	}

	Expect(subdirs).To(HaveLen(1), "Expected exactly one subdirectory, found: %v", subdirs)
	return path.Join(destDir, subdirs[0])
}

func mustGatherImage() string {
	injected := os.Getenv("E2E_MUST_GATHER_IMAGE")
	if injected == "" {
		return defaultMustGatherImage
	}
	return injected
}

// BeValidResourceFile checks if the file exists and if it is a valid YAML file.
func BeValidResourceFile() types.GomegaMatcher {
	return &validResourceFile{}
}

type validResourceFile struct{}

func (matcher *validResourceFile) Match(actual any) (success bool, err error) {
	filePath, ok := actual.(string)
	if !ok {
		return false, fmt.Errorf("BeValidResourceFile matcher expects a string (file path)")
	}

	filePath = filepath.Clean(filePath)
	f, err := os.Open(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to read file: %v", err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	var data map[string]any
	if err := decoder.Decode(&data); err != nil {
		return false, fmt.Errorf("failed parsing supposed YAML file: %v", err)
	}

	_, exists := data["kind"]
	return exists, nil
}

func (matcher *validResourceFile) FailureMessage(actual any) string {
	return fmt.Sprintf("Expected\n\t%v\nto be a valid YAML resource file", actual)
}

func (matcher *validResourceFile) NegatedFailureMessage(actual any) string {
	return fmt.Sprintf("Expected\n\t%v\nnot to be a valid YAML resource file", actual)
}
