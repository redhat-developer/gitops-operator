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

			By("checking that the expected CSV matches the actual CSV on the cluster")

			csvString := `
apiVersion: operators.coreos.com/v1alpha1
kind: ClusterServiceVersion
metadata:
  name: openshift-gitops-operator.v1.16.0
  namespace: openshift-operators
spec:
  install:
    spec:
      clusterPermissions:
      - rules:
        - apiGroups:
          - ""
          resources:
          - configmaps
          - endpoints
          - events
          - namespaces
          - pods
          - secrets
          - serviceaccounts
          - services
          - services/finalizers
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - ""
          resources:
          - configmaps
          - endpoints
          - events
          - persistentvolumeclaims
          - pods
          - secrets
          - serviceaccounts
          - services
          - services/finalizers
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - ""
          resources:
          - deployments
          verbs:
          - get
          - list
          - watch
        - apiGroups:
          - ""
          resources:
          - namespaces
          - resourcequotas
          verbs:
          - create
          - delete
          - get
          - list
          - update
          - watch
        - apiGroups:
          - ""
          resources:
          - pods/eviction
          verbs:
          - create
        - apiGroups:
          - ""
          resources:
          - pods/log
          verbs:
          - get
        - apiGroups:
          - ""
          resources:
          - podtemplates
          verbs:
          - get
          - list
          - watch
        - apiGroups:
          - apiextensions.k8s.io
          resources:
          - customresourcedefinitions
          verbs:
          - get
          - list
          - watch
        - apiGroups:
          - apiregistration.k8s.io
          resources:
          - apiservices
          verbs:
          - get
          - list
        - apiGroups:
          - appmesh.k8s.aws
          resources:
          - virtualnodes
          - virtualrouters
          verbs:
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - appmesh.k8s.aws
          resources:
          - virtualservices
          verbs:
          - get
          - list
          - watch
        - apiGroups:
          - apps
          resources:
          - daemonsets
          - deployments
          - replicasets
          - statefulsets
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - apps
          resources:
          - deployments
          - podtemplates
          - replicasets
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - apps
          resources:
          - deployments/finalizers
          verbs:
          - update
        - apiGroups:
          - apps
          resourceNames:
          - gitops-operator
          resources:
          - deployments/finalizers
          verbs:
          - update
        - apiGroups:
          - apps.openshift.io
          resources:
          - '*'
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - argoproj.io
          resources:
          - analysisruns
          - analysisruns/finalizers
          - experiments
          - experiments/finalizers
          verbs:
          - create
          - delete
          - deletecollection
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - argoproj.io
          resources:
          - analysistemplates
          verbs:
          - create
          - delete
          - deletecollection
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - argoproj.io
          resources:
          - applications
          - appprojects
          - argocds
          - argocds/finalizers
          - argocds/status
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - argoproj.io
          resources:
          - clusteranalysistemplates
          verbs:
          - create
          - delete
          - deletecollection
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - argoproj.io
          resources:
          - notificationsconfigurations
          - notificationsconfigurations/finalizers
          verbs:
          - '*'
        - apiGroups:
          - argoproj.io
          resources:
          - rolloutmanagers
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - argoproj.io
          resources:
          - rolloutmanagers/finalizers
          verbs:
          - update
        - apiGroups:
          - argoproj.io
          resources:
          - rolloutmanagers/status
          verbs:
          - get
          - patch
          - update
        - apiGroups:
          - argoproj.io
          resources:
          - rollouts
          - rollouts/finalizers
          - rollouts/scale
          - rollouts/status
          verbs:
          - create
          - delete
          - deletecollection
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - autoscaling
          resources:
          - horizontalpodautoscalers
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - batch
          resources:
          - cronjobs
          - jobs
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - batch
          resources:
          - jobs
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - config.openshift.io
          resources:
          - clusterversions
          verbs:
          - get
          - list
          - watch
        - apiGroups:
          - console.openshift.io
          resources:
          - consoleclidownloads
          verbs:
          - create
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - console.openshift.io
          resources:
          - consolelinks
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - console.openshift.io
          resources:
          - consoleplugins
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - coordination.k8s.io
          resources:
            - leases
          verbs:
            - create
            - get
            - update
        - apiGroups:
          - elbv2.k8s.aws
          resources:
            - targetgroupbindings
          verbs:
            - get
            - list
        - apiGroups:
          - extensions
          resources:
          - ingresses
          verbs:
          - create
          - get
          - list
          - patch
          - watch
        - apiGroups:
          - getambassador.io
          resources:
          - ambassadormappings
          - mappings
          verbs:
          - create
          - delete
          - get
          - list
          - update
          - watch
        - apiGroups:
          - monitoring.coreos.com
          resources:
          - prometheuses
          - prometheusrules
          - servicemonitors
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - networking.istio.io
          resources:
          - destinationrules
          - virtualservices
          verbs:
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
            - networking.k8s.io
          resources:
          - ingresses
          verbs:
          - create
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
            - networking.k8s.io
          resources:
          - ingresses
          - networkpolicies
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - oauth.openshift.io
          resources:
          - oauthclients
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - operators.coreos.com
          resources:
          - clusterserviceversions
          - operatorgroups
          - subscriptions
          verbs:
          - create
          - get
          - list
          - watch
        - apiGroups:
          - pipelines.openshift.io
          resources:
          - '*'
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - pipelines.openshift.io
          resources:
          - gitopsservices
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - pipelines.openshift.io
          resources:
          - gitopsservices/finalizers
          verbs:
          - update
        - apiGroups:
          - pipelines.openshift.io
          resources:
          - gitopsservices/status
          verbs:
          - get
          - patch
          - update
        - apiGroups:
          - rbac.authorization.k8s.io
          resources:
          - '*'
          verbs:
          - bind
          - create
          - delete
          - deletecollection
          - escalate
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - rbac.authorization.k8s.io
          resources:
          - clusterrolebindings
          - clusterroles
          verbs:
          - bind
          - create
          - delete
          - deletecollection
          - escalate
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - rbac.authorization.k8s.io
          resources:
          - rolebindings
          - roles
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - route.openshift.io
          resources:
          - '*'
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - route.openshift.io
          resources:
          - routes
          - routes/custom-host
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - split.smi-spec.io
          resources:
          - trafficsplits
          verbs:
          - create
          - get
          - patch
          - update
          - watch
        - apiGroups:
          - template.openshift.io
          resources:
          - templateconfigs
          - templateinstances
          - templates
          verbs:
          - create
          - delete
          - get
          - list
          - patch
          - update
          - watch
        - apiGroups:
          - traefik.containo.us
          resources:
          - traefikservices
          verbs:
          - get
          - update
          - watch
        - apiGroups:
          - x.getambassador.io
          resources:
          - ambassadormappings
          - mappings
          verbs:
          - create
          - delete
          - get
          - list
          - update
          - watch
        - apiGroups:
          - authentication.k8s.io
          resources:
          - tokenreviews
          verbs:
          - create
        - apiGroups:
          - authorization.k8s.io
          resources:
          - subjectaccessreviews
          verbs:
          - create`

			expectedCsv := &olmv1alpha1.ClusterServiceVersion{}

			Expect(yaml.UnmarshalStrict([]byte(csvString), expectedCsv)).To(Succeed())

			By("looking for a ClusterServiceVersion for openshift-gitops across all namespaces")
			gitopsCSVsFound := []olmv1alpha1.ClusterServiceVersion{}
			var csvList olmv1alpha1.ClusterServiceVersionList
			Expect(k8sClient.List(ctx, &csvList)).To(Succeed())
			for index := range csvList.Items {
				csv := csvList.Items[index]
				if strings.Contains(csv.Name, "openshift-gitops-operator") {
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

			Expect(expectedCsv.Spec.InstallStrategy.StrategySpec.ClusterPermissions).To(HaveLen(1))

			Expect(actualCsv.Spec.InstallStrategy.StrategySpec.ClusterPermissions).To(Equal(expectedCsv.Spec.InstallStrategy.StrategySpec.ClusterPermissions))

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
