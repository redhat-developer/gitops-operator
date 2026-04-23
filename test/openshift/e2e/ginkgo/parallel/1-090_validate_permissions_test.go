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
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-090_validate_permissions", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("ensure the GitOps Operator CSV has the correct cluster permissions and gitopsservices CRD has expected properties", func() {

			if fixture.EnvCI() {
				Skip("AFAICT CSV does not exist when running E2E tests from gitops-operator repo")
				return
			}

			if fixture.EnvLocalRun() || fixture.EnvNonOLM() {
				Skip("CSV does not exist in the local run or non-OLM case, so we skip")
				return
			}

			By("looking for a ClusterServiceVersion for openshift-gitops across all namespaces")
			gitopsCSVsFound := []olmv1alpha1.ClusterServiceVersion{}
			var csvList olmv1alpha1.ClusterServiceVersionList
			Expect(k8sClient.List(ctx, &csvList)).To(Succeed())
			for index := range csvList.Items {
				csv := csvList.Items[index]
				if strings.Contains(csv.Name, "openshift-gitops-operator") {
					// OLM copies CSVs to other namespaces; skip those copies
					if _, copied := csv.Labels["olm.copiedFrom"]; copied {
						continue
					}
					gitopsCSVsFound = append(gitopsCSVsFound, csv)
				}
			}
			By("if more than one possible CSV is found, we will fail.")
			Expect(gitopsCSVsFound).To(HaveLen(1), fmt.Sprintf("Exactly one CSV should found: %v", gitopsCSVsFound))

			actualCsv := &olmv1alpha1.ClusterServiceVersion{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gitopsCSVsFound[0].Name,
					Namespace: gitopsCSVsFound[0].Namespace,
				},
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(actualCsv), actualCsv)).To(Succeed())

			// We don't need to compare the .serviceAccountName field so set it to empty
			Expect(actualCsv.Spec.InstallStrategy.StrategySpec.ClusterPermissions).To(HaveLen(1))
			actualCsv.Spec.InstallStrategy.StrategySpec.ClusterPermissions[0].ServiceAccountName = ""

			snapshotPath := "../snapshots/valid_csv_permissions.yaml"

			if os.Getenv("E2E_UPDATE_SNAPSHOTS") == "1" {
				By("updating snapshot file with actual CSV cluster permissions")
				data, marshalErr := yaml.Marshal(actualCsv.Spec.InstallStrategy.StrategySpec.ClusterPermissions)
				Expect(marshalErr).NotTo(HaveOccurred())
				Expect(os.MkdirAll(filepath.Dir(snapshotPath), 0755)).To(Succeed())
				Expect(os.WriteFile(snapshotPath, data, 0644)).To(Succeed())
			}

			By("checking that the expected CSV cluster permissions match the actual CSV on the cluster")

			snapshotData, readErr := os.ReadFile(snapshotPath)
			Expect(readErr).NotTo(HaveOccurred(), "snapshot file not found at %s; run with E2E_UPDATE_SNAPSHOTS=1 to create it", snapshotPath)

			var snapshotPermissions []olmv1alpha1.StrategyDeploymentPermissions
			Expect(yaml.Unmarshal(snapshotData, &snapshotPermissions)).To(Succeed())

			Expect(actualCsv.Spec.InstallStrategy.StrategySpec.ClusterPermissions).To(Equal(snapshotPermissions))

			By("checking that the specific fields in gitopsservices.pipelines.openshift.io CRD that we are looking for are present and have the expected values")

			gitopsServiceCRDYAML := `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: gitopsservices.pipelines.openshift.io
spec:
  versions:
  - name: v1alpha1
    schema:
      openAPIV3Schema:
        properties:
          spec:
            description: GitopsServiceSpec defines the desired state of GitopsService
            properties:
              nodeSelector:
                additionalProperties:
                  type: string
                description: NodeSelector is a map of key value pairs used for node
                  selection in the default workloads
                type: object
              tolerations:
                description: Tolerations allow the default workloads to schedule onto
                  nodes with matching taints`

			expectedCRD := &apiextensionsv1.CustomResourceDefinition{}
			Expect(yaml.UnmarshalStrict([]byte(gitopsServiceCRDYAML), expectedCRD)).To(Succeed())

			fmt.Println("expectedCRD", expectedCRD)

			actualCRD := &apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gitopsservices.pipelines.openshift.io",
				},
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(actualCRD), actualCRD)).To(Succeed())

			Expect(expectedCRD.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["description"]).To(Equal(actualCRD.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["description"]))

			Expect(expectedCRD.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["tolerations"]).To(Equal(actualCRD.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["tolerations"]))

			Expect(expectedCRD.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["nodeSelector"]).To(Equal(actualCRD.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["nodeSelector"]))

			Expect(expectedCRD.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["tolerations"]).To(Equal(actualCRD.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["tolerations"]))

		})

	})
})
