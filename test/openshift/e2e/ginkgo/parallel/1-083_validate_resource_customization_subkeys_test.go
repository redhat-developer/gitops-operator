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

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	configmapFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/configmap"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-083_validate_resource_customization_subkeys", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()

			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("validates setting Argo CD CR .spec.resourceIgnoreDifferences/resourceActions/resourceHealthChecks will cause the corresponding setting to be set on argocd-cm ConfigMap", func() {

			By("creating a basic Argo CD instance with .spec.resourceIgnoreDifferences/resourceActions/resourceHealthChecks set with custom values")
			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			argoCDYAML := `
metadata:
  name: example-argocd
spec:
  resourceIgnoreDifferences:
    all:
      jqPathExpressions:
          - xyz
          - abc
      jsonPointers:
          - xyz
          - abc
      managedFieldsManagers:
          - xyz
          - abc
    resourceIdentifiers:
      - group: apps
        kind: deployments
        customization:
          jqPathExpressions:
            - xyz
            - abc
          jsonPointers:
            - xyz
            - abc
          managedFieldsManagers:
            - xyz
            - abc
      - group: batch
        kind: jobs
        customization:
          jqPathExpressions:
            - xyz
            - abc
          jsonPointers:
            - xyz
            - abc
          managedFieldsManagers:
            - xyz
            - abc
  resourceHealthChecks:
    - group: certmanager.k8s.io
      kind: Certificate
      check: |
        hs = {}
        if obj.status ~= nil then
          if obj.status.conditions ~= nil then
            for i, condition in ipairs(obj.status.conditions) do
              if condition.type == "Ready" and condition.status == "False" then
                hs.status = "Degraded"
                hs.message = condition.message
                return hs
              end
              if condition.type == "Ready" and condition.status == "True" then
                hs.status = "Healthy"
                hs.message = condition.message
                return hs
              end
            end
          end
        end
        hs.status = "Progressing"
        hs.message = "Waiting for certificate"
        return hs
  resourceActions:
    - group: apps
      kind: Deployment
      action: |
        discovery.lua: |
        actions = {}
        actions["restart"] = {}
        return actions
        definitions:
        - name: restart
          # Lua Script to modify the obj
          action.lua: |
            local os = require("os")
            if obj.spec.template.metadata == nil then
                obj.spec.template.metadata = {}
            end
            if obj.spec.template.metadata.annotations == nil then
                obj.spec.template.metadata.annotations = {}
            end
            obj.spec.template.metadata.annotations["kubectl.kubernetes.io/restartedAt"] = os.date("!%Y-%m-%dT%XZ")
            return obj`

			// We unmarshal YAML into ArgoCD CR, so that we don't have to convert it into Go structs (it would be painful)
			argoCD := &argov1beta1api.ArgoCD{}
			Expect(yaml.UnmarshalStrict([]byte(argoCDYAML), &argoCD)).To(Succeed())
			argoCD.ObjectMeta.Namespace = ns.Name
			Expect(k8sClient.Create(ctx, argoCD)).To(Succeed())

			By("waiting for ArgoCD CR to be reconciled and the instance to be ready")
			Eventually(argoCD, "3m", "5s").Should(argocdFixture.BeAvailable())

			By("waiting for each of the .data fields of argocd-cm ConfigMap to have expected value")
			configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-cm", Namespace: ns.Name}}
			Eventually(configMap).Should(k8sFixture.ExistByName())

			expectedDataFieldYaml := `
  admin.enabled: "true"
  application.instanceLabelKey: app.kubernetes.io/instance
  application.resourceTrackingMethod: label
  configManagementPlugins: ""
  ga.anonymizeusers: "false"
  ga.trackingid: ""
  help.chatText: ""
  help.chatUrl: ""
  kustomize.buildOptions: ""
  oidc.config: ""
  repositories: ""
  repository.credentials: ""
  resource.customizations.actions.apps_Deployment: |
    discovery.lua: |
    actions = {}
    actions["restart"] = {}
    return actions
    definitions:
    - name: restart
      # Lua Script to modify the obj
      action.lua: |
        local os = require("os")
        if obj.spec.template.metadata == nil then
            obj.spec.template.metadata = {}
        end
        if obj.spec.template.metadata.annotations == nil then
            obj.spec.template.metadata.annotations = {}
        end
        obj.spec.template.metadata.annotations["kubectl.kubernetes.io/restartedAt"] = os.date("!%Y-%m-%dT%XZ")
        return obj
  resource.customizations.health.certmanager.k8s.io_Certificate: |
    hs = {}
    if obj.status ~= nil then
      if obj.status.conditions ~= nil then
        for i, condition in ipairs(obj.status.conditions) do
          if condition.type == "Ready" and condition.status == "False" then
            hs.status = "Degraded"
            hs.message = condition.message
            return hs
          end
          if condition.type == "Ready" and condition.status == "True" then
            hs.status = "Healthy"
            hs.message = condition.message
            return hs
          end
        end
      end
    end
    hs.status = "Progressing"
    hs.message = "Waiting for certificate"
    return hs
  resource.customizations.ignoreDifferences.all: |
    jqpathexpressions:
    - xyz
    - abc
    jsonpointers:
    - xyz
    - abc
    managedfieldsmanagers:
    - xyz
    - abc
  resource.customizations.ignoreDifferences.apps_deployments: |
    jqpathexpressions:
    - xyz
    - abc
    jsonpointers:
    - xyz
    - abc
    managedfieldsmanagers:
    - xyz
    - abc
  resource.customizations.ignoreDifferences.batch_jobs: |
    jqpathexpressions:
    - xyz
    - abc
    jsonpointers:
    - xyz
    - abc
    managedfieldsmanagers:
    - xyz
    - abc
`
			var expectedDataObj map[string]string
			Expect(yaml.Unmarshal([]byte(expectedDataFieldYaml), &expectedDataObj)).To(Succeed())

			for k, v := range expectedDataObj {
				Eventually(configMap).Should(configmapFixture.HaveStringDataKeyValue(k, v), "unable to locate '"+k+"': '"+v+"'")
			}

		})

	})
})
