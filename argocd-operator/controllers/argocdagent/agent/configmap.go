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

package agent

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argoproj "github.com/argoproj-labs/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/argoproj-labs/gitops-operator/argocd-operator/common"
	"github.com/argoproj-labs/gitops-operator/argocd-operator/controllers/argoutil"
)

// agentParamsKeys are the argocd-agent-params keys derived from the Agent section of the ArgoCD
// CR.
var agentParamsKeys = []string{
	common.ArgoCDAgentAgentLogLevelKey,
	common.ArgoCDAgentAgentFullDetailKey,
	common.ArgoCDAgentAgentPProfEnabledKey,
}

// ReconcileAgentParamsConfigMap manages the agent's keys in the argocd-agent-params ConfigMap,
// which the agent watches for settings it applies without restarting.
//
// The principal reconciles a ConfigMap of the same name. The two never collide in a real
// topology, because the principal runs on the control plane cluster and the agent on a workload
// cluster, and the operator refuses to enable both in one namespace anyway. They are kept apart
// by the app.kubernetes.io/component label so that whichever of the two is disabled cannot
// delete the other's ConfigMap.
func ReconcileAgentParamsConfigMap(c client.Client, compName string, cr *argoproj.ArgoCD, scheme *runtime.Scheme) error {
	return argoutil.ReconcileAgentParamsConfigMap(c, log, cr, scheme, argoutil.AgentParamsConfigMapOptions{
		Component:   compName,
		Labels:      buildLabelsForAgent(cr.Name, compName),
		ManagedKeys: agentParamsKeys,
		Desired:     buildAgentParams(cr),
		Enabled:     isAgentEnabled(cr),
	})
}

// buildAgentParams returns a value for every key the operator manages, falling back to the
// agent's own default where the ArgoCD CR says nothing.
//
// Every key is written on every reconcile, never pruned while the agent is enabled, because the
// deployment sources these settings from this ConfigMap with a ConfigMapKeyRef.
func buildAgentParams(cr *argoproj.ArgoCD) map[string]string {
	if !isAgentEnabled(cr) {
		return map[string]string{}
	}

	agent := cr.Spec.ArgoCDAgent.Agent

	var pprofEnabled *bool
	if agent.PProf != nil {
		pprofEnabled = agent.PProf.Enabled
	}

	return map[string]string{
		common.ArgoCDAgentAgentLogLevelKey:     agent.LogLevel,
		common.ArgoCDAgentAgentFullDetailKey:   argoutil.AgentFullDetailParam(agent.LogFullDetail),
		common.ArgoCDAgentAgentPProfEnabledKey: argoutil.AgentPProfEnabledParam(pprofEnabled),
	}
}

func isAgentEnabled(cr *argoproj.ArgoCD) bool {
	return hasAgent(cr) && cr.Spec.ArgoCDAgent.Agent.IsEnabled()
}
