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

package argoutil

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	argoproj "github.com/argoproj-labs/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/argoproj-labs/gitops-operator/argocd-operator/common"
)

func TestReconcileAgentParamsConfigMap_RejectsOptionsMissingTheComponentLabel(t *testing.T) {
	// The component label is what stops the sibling component's reconciler from deleting this
	// ConfigMap. Creating one without it would start a delete/create loop, so the reconcile
	// must fail loudly instead.
	sch := runtime.NewScheme()
	require.NoError(t, scheme.AddToScheme(sch))
	require.NoError(t, argoproj.AddToScheme(sch))

	cr := &argoproj.ArgoCD{ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: "argocd"}}
	cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(cr).Build()

	errMsg := fmt.Sprintf("cannot reconcile %s configmap for component %q: missing label %s=%s",
		common.ArgoCDAgentParamsConfigMapName, "principal", common.ArgoCDKeyComponent, "principal",
	)
	err := ReconcileAgentParamsConfigMap(cl, log, cr, sch, AgentParamsConfigMapOptions{
		Component: "principal",
		Labels:    map[string]string{common.ArgoCDKeyName: "argocd-agent-principal"},
		Enabled:   true,
	})
	assert.ErrorContains(t, err, errMsg)

	err = ReconcileAgentParamsConfigMap(cl, log, cr, sch, AgentParamsConfigMapOptions{
		Component: "principal",
		Labels:    map[string]string{common.ArgoCDKeyComponent: "agent"},
		Enabled:   true,
	})
	assert.ErrorContains(t, err, errMsg)

	require.NoError(t, ReconcileAgentParamsConfigMap(cl, log, cr, sch, AgentParamsConfigMapOptions{
		Component: "principal",
		Labels:    map[string]string{common.ArgoCDKeyComponent: "principal"},
		Enabled:   true,
	}))
}

func TestAgentFullDetailParam_PassesCategoriesThroughVerbatim(t *testing.T) {
	// Not checked against a known list: the component validates these itself
	assert.Equal(t, "events,informers", AgentFullDetailParam([]string{"events", "informers"}))
	assert.Equal(t, "grpc", AgentFullDetailParam([]string{"grpc"}), "unknown category must survive")
	assert.Equal(t, "", AgentFullDetailParam(nil), "unset means off")
	assert.Equal(t, "", AgentFullDetailParam([]string{}), "empty means off")
}

func TestAgentPProfEnabledParam_NeverEmpty(t *testing.T) {
	// argocd-agent runs strconv.ParseBool on this key and warns on anything it cannot parse,
	// which an empty string would trigger on every reconcile.
	assert.Equal(t, "false", AgentPProfEnabledParam(nil))
	assert.Equal(t, "false", AgentPProfEnabledParam(new(false)))
	assert.Equal(t, "true", AgentPProfEnabledParam(new(true)))
}
