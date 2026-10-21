// Copyright 2026 ArgoCD Operator Developers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package argocdagent

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argoutil"
)

// principalParamsKeys are the argocd-agent-params keys derived from the Principal section of the
// ArgoCD CR.
var principalParamsKeys = []string{
	common.ArgoCDAgentPrincipalLogLevelKey,
	common.ArgoCDAgentPrincipalFullDetailKey,
	common.ArgoCDAgentPrincipalPProfEnabledKey,
}

// ReconcilePrincipalParamsConfigMap manages the principal's keys in the argocd-agent-params
// ConfigMap, which the principal watches for settings it applies without restarting.
func ReconcilePrincipalParamsConfigMap(c client.Client, compName string, cr *argoproj.ArgoCD, scheme *runtime.Scheme) error {
	return argoutil.ReconcileAgentParamsConfigMap(c, log, cr, scheme, argoutil.AgentParamsConfigMapOptions{
		Component:   compName,
		Labels:      buildLabelsForAgentPrincipal(cr.Name, compName),
		ManagedKeys: principalParamsKeys,
		Desired:     buildPrincipalParams(cr),
		Enabled:     isPrincipalEnabled(cr),
	})
}

// buildPrincipalParams returns a value for every key the operator manages, falling back to the
// principal's own default where the ArgoCD CR says nothing.
//
// Every key is written on every reconcile, never pruned while the principal is enabled, because
// the deployment sources these settings from this ConfigMap with a ConfigMapKeyRef.
func buildPrincipalParams(cr *argoproj.ArgoCD) map[string]string {
	if !isPrincipalEnabled(cr) {
		return map[string]string{}
	}

	principal := cr.Spec.ArgoCDAgent.Principal

	var pprofEnabled *bool
	if principal.PProf != nil {
		pprofEnabled = principal.PProf.Enabled
	}

	return map[string]string{
		common.ArgoCDAgentPrincipalLogLevelKey:     principal.LogLevel,
		common.ArgoCDAgentPrincipalFullDetailKey:   argoutil.AgentFullDetailParam(principal.LogFullDetail),
		common.ArgoCDAgentPrincipalPProfEnabledKey: argoutil.AgentPProfEnabledParam(pprofEnabled),
	}
}

func isPrincipalEnabled(cr *argoproj.ArgoCD) bool {
	return hasPrincipal(cr) && cr.Spec.ArgoCDAgent.Principal.IsEnabled()
}
