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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argoproj "github.com/argoproj-labs/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/argoproj-labs/gitops-operator/argocd-operator/common"
	"github.com/argoproj-labs/gitops-operator/argocd-operator/controllers/argocdagent"
)

func withAgentLogLevel(level string) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		withAgentEnabled(true)(a)
		a.Spec.ArgoCDAgent.Agent.LogLevel = level
	}
}

func withAgentLogFullDetail(categories ...string) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		withAgentEnabled(true)(a)
		a.Spec.ArgoCDAgent.Agent.LogFullDetail = categories
	}
}

func withAgentPProf(pprof *argoproj.ArgoCDAgentPProfSpec) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		withAgentEnabled(true)(a)
		a.Spec.ArgoCDAgent.Agent.PProf = pprof
	}
}

func getParamsConfigMap(t *testing.T, cl client.Client, ns string) *corev1.ConfigMap {
	t.Helper()
	cm := &corev1.ConfigMap{}
	err := cl.Get(context.TODO(), types.NamespacedName{
		Name:      common.ArgoCDAgentParamsConfigMapName,
		Namespace: ns,
	}, cm)
	require.NoError(t, err)
	return cm
}

func TestReconcileAgentParamsConfigMap_AgentDisabled_IsNotCreated(t *testing.T) {
	cr := makeTestArgoCD(withAgentEnabled(false))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcileAgentParamsConfigMap(cl, testAgentCompName, cr, sch))

	cm := &corev1.ConfigMap{}
	err := cl.Get(context.TODO(), types.NamespacedName{
		Name:      common.ArgoCDAgentParamsConfigMapName,
		Namespace: cr.Namespace,
	}, cm)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcileAgentParamsConfigMap_AgentEnabled_NoFieldsSet(t *testing.T) {
	// The ConfigMap is created so the knobs have a home, but it carries no managed keys:
	// writing defaults would shadow anything supplied through the Agent `env` field.
	cr := makeTestArgoCD(withAgentEnabled(true))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcileAgentParamsConfigMap(cl, testAgentCompName, cr, sch))

	cm := getParamsConfigMap(t, cl, cr.Namespace)
	// Every managed key carries the agent's own default rather than being left out, so the
	// deployment can safely source them with a ConfigMapKeyRef.
	assert.Equal(t, map[string]string{
		common.ArgoCDAgentAgentLogLevelKey:     "",
		common.ArgoCDAgentAgentFullDetailKey:   "",
		common.ArgoCDAgentAgentPProfEnabledKey: "false",
	}, cm.Data)
	assert.Equal(t, testAgentCompName, cm.Labels[common.ArgoCDKeyComponent],
		"the component label is what keeps the principal reconciler from deleting this ConfigMap")

	owner := metav1.GetControllerOf(cm)
	require.NotNil(t, owner)
	assert.Equal(t, cr.Name, owner.Name)
}

func TestReconcileAgentParamsConfigMap_AgentFieldsAreWritten(t *testing.T) {
	cr := makeTestArgoCD(
		withAgentLogLevel("trace"),
		withAgentLogFullDetail("all"),
		withAgentPProf(&argoproj.ArgoCDAgentPProfSpec{Enabled: new(false)}),
	)
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcileAgentParamsConfigMap(cl, testAgentCompName, cr, sch))

	assert.Equal(t, map[string]string{
		common.ArgoCDAgentAgentLogLevelKey:     "trace",
		common.ArgoCDAgentAgentFullDetailKey:   "all",
		common.ArgoCDAgentAgentPProfEnabledKey: "false",
	}, getParamsConfigMap(t, cl, cr.Namespace).Data)
}

func TestReconcileAgentParamsConfigMap_PrincipalKeysAreNeverWritten(t *testing.T) {
	// The agent reconciler is authoritative only for the agent-prefixed keys.
	cr := makeTestArgoCD(withAgentLogLevel("trace"))
	cr.Spec.ArgoCDAgent.Principal = &argoproj.PrincipalSpec{
		Enabled:  new(false),
		LogLevel: "debug",
	}

	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcileAgentParamsConfigMap(cl, testAgentCompName, cr, sch))

	assert.Equal(t, map[string]string{
		common.ArgoCDAgentAgentLogLevelKey:     "trace",
		common.ArgoCDAgentAgentFullDetailKey:   "",
		common.ArgoCDAgentAgentPProfEnabledKey: "false",
	}, getParamsConfigMap(t, cl, cr.Namespace).Data)
}

func TestReconcileAgentParamsConfigMap_UnsettingAFieldRevertsToTheDefault(t *testing.T) {
	cr := makeTestArgoCD(withAgentPProf(&argoproj.ArgoCDAgentPProfSpec{Enabled: new(true)}))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcileAgentParamsConfigMap(cl, testAgentCompName, cr, sch))
	assert.Equal(t, "true", getParamsConfigMap(t, cl, cr.Namespace).Data[common.ArgoCDAgentAgentPProfEnabledKey])

	cr.Spec.ArgoCDAgent.Agent.PProf = nil
	require.NoError(t, ReconcileAgentParamsConfigMap(cl, testAgentCompName, cr, sch))

	// Written as "false", not removed: the key must never disappear while the agent is enabled,
	// or a pod that restarted while pprof was on would fall back to its env baseline and keep
	// the endpoint open forever.
	assert.Equal(t, "false", getParamsConfigMap(t, cl, cr.Namespace).Data[common.ArgoCDAgentAgentPProfEnabledKey])
}

func TestReconcileAgentParamsConfigMap_RevertsManualEditAndPreservesUnmanagedKeys(t *testing.T) {
	cr := makeTestArgoCD(withAgentLogLevel("trace"))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcileAgentParamsConfigMap(cl, testAgentCompName, cr, sch))

	cm := getParamsConfigMap(t, cl, cr.Namespace)
	cm.Data[common.ArgoCDAgentAgentLogLevelKey] = "debug"
	cm.Data["agent.some.future.setting"] = "42"
	require.NoError(t, cl.Update(context.TODO(), cm))

	require.NoError(t, ReconcileAgentParamsConfigMap(cl, testAgentCompName, cr, sch))

	cm = getParamsConfigMap(t, cl, cr.Namespace)
	assert.Equal(t, "trace", cm.Data[common.ArgoCDAgentAgentLogLevelKey], "the CR is the source of truth")
	assert.Equal(t, "42", cm.Data["agent.some.future.setting"], "unmanaged keys must survive")
}

func TestReconcileAgentParamsConfigMap_DeletedWhenAgentIsDisabled(t *testing.T) {
	cr := makeTestArgoCD(withAgentLogLevel("trace"))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcileAgentParamsConfigMap(cl, testAgentCompName, cr, sch))
	getParamsConfigMap(t, cl, cr.Namespace)

	withAgentEnabled(false)(cr)
	require.NoError(t, ReconcileAgentParamsConfigMap(cl, testAgentCompName, cr, sch))

	cm := &corev1.ConfigMap{}
	err := cl.Get(context.TODO(), types.NamespacedName{
		Name:      common.ArgoCDAgentParamsConfigMapName,
		Namespace: cr.Namespace,
	}, cm)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcileAgentParamsConfigMap_DoesNotTouchThePrincipalsConfigMap(t *testing.T) {
	// both reconcilers run against the same namespace on every reconcile, even
	// though only one component can be enabled. Without the component-label check the disabled
	// agent reconciler would delete the enabled principal's ConfigMap, which the principal
	// would immediately recreate - a delete/create loop driven by the operator's own ConfigMap
	// watch.
	cr := makeTestArgoCD(withAgentEnabled(false))
	cr.Spec.ArgoCDAgent.Principal = &argoproj.PrincipalSpec{Enabled: new(true), LogLevel: "debug"}

	principalCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.ArgoCDAgentParamsConfigMapName,
			Namespace: cr.Namespace,
			Labels:    map[string]string{common.ArgoCDKeyComponent: "principal"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: argoproj.GroupVersion.String(),
				Kind:       "ArgoCD",
				Name:       cr.Name,
				UID:        cr.UID,
				Controller: new(true),
			}},
		},
		Data: map[string]string{common.ArgoCDAgentPrincipalLogLevelKey: "debug"},
	}

	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr, principalCM})

	require.NoError(t, ReconcileAgentParamsConfigMap(cl, testAgentCompName, cr, sch))

	cm := getParamsConfigMap(t, cl, cr.Namespace)
	assert.Equal(t, "debug", cm.Data[common.ArgoCDAgentPrincipalLogLevelKey],
		"the agent reconciler must leave the principal's ConfigMap and keys alone")
	assert.Equal(t, "principal", cm.Labels[common.ArgoCDKeyComponent])
}

func TestReconcileAgentParamsConfigMap_IsIdempotent(t *testing.T) {
	cr := makeTestArgoCD(
		withAgentLogLevel("trace"),
		withAgentLogFullDetail("events"),
		withAgentPProf(&argoproj.ArgoCDAgentPProfSpec{Enabled: new(true)}),
	)
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcileAgentParamsConfigMap(cl, testAgentCompName, cr, sch))
	first := getParamsConfigMap(t, cl, cr.Namespace)

	require.NoError(t, ReconcileAgentParamsConfigMap(cl, testAgentCompName, cr, sch))
	second := getParamsConfigMap(t, cl, cr.Namespace)

	assert.Equal(t, first.Data, second.Data)
	// A no-op reconcile must not issue an Update, or the controller would hot-loop against its
	// own ConfigMap watch.
	assert.Equal(t, first.ResourceVersion, second.ResourceVersion)
}

func TestReconcileAgentParamsConfigMap_WritesTheLogLevelVerbatim(t *testing.T) {
	cr := makeTestArgoCD(withAgentLogLevel("some-new-subsystem=trace"))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcileAgentParamsConfigMap(cl, testAgentCompName, cr, sch))

	assert.Equal(t, "some-new-subsystem=trace",
		getParamsConfigMap(t, cl, cr.Namespace).Data[common.ArgoCDAgentAgentLogLevelKey])
}

// TestAgentAndPrincipalParamsConfigMapsConverge drives both reconcilers in the same order
// reconcileArgoCDAgent does, repeatedly, against a single namespace.
//
// This is the test that actually answers "can the two reconcilers tell their ConfigMaps apart?".
// The per-reconciler tests above each check one half against a hand-built sibling ConfigMap;
// only running the real sequence proves the pair reaches a fixed point instead of the disabled
// component deleting what the enabled one just created, forever.
func TestAgentAndPrincipalParamsConfigMapsConverge(t *testing.T) {
	for _, tt := range []struct {
		name             string
		principalEnabled bool
		agentEnabled     bool
		wantData         map[string]string
		wantComponent    string
	}{
		{
			name:             "principal enabled, agent disabled",
			principalEnabled: true,
			wantData: map[string]string{
				common.ArgoCDAgentPrincipalLogLevelKey:     "debug",
				common.ArgoCDAgentPrincipalFullDetailKey:   "",
				common.ArgoCDAgentPrincipalPProfEnabledKey: "false",
			},
			wantComponent: "principal",
		},
		{
			name:         "agent enabled, principal disabled",
			agentEnabled: true,
			wantData: map[string]string{
				common.ArgoCDAgentAgentLogLevelKey:     "trace",
				common.ArgoCDAgentAgentFullDetailKey:   "",
				common.ArgoCDAgentAgentPProfEnabledKey: "false",
			},
			wantComponent: "agent",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cr := makeTestArgoCD()
			cr.Spec.ArgoCDAgent = &argoproj.ArgoCDAgentSpec{
				Principal: &argoproj.PrincipalSpec{Enabled: &tt.principalEnabled, LogLevel: "debug"},
				Agent:     &argoproj.AgentSpec{Enabled: &tt.agentEnabled, LogLevel: "trace"},
			}

			sch := makeTestReconcilerScheme()
			cl := makeTestReconcilerClient(sch, []client.Object{cr})

			reconcileBoth := func() {
				require.NoError(t, argocdagent.ReconcilePrincipalParamsConfigMap(cl, "principal", cr, sch))
				require.NoError(t, ReconcileAgentParamsConfigMap(cl, "agent", cr, sch))
			}

			reconcileBoth()
			first := getParamsConfigMap(t, cl, cr.Namespace)
			assert.Equal(t, tt.wantData, first.Data)
			assert.Equal(t, tt.wantComponent, first.Labels[common.ArgoCDKeyComponent])

			// Several more passes: the ConfigMap must survive untouched, and nothing may issue
			// a write. A changing ResourceVersion here would mean the two reconcilers are
			// fighting, which under the operator's ConfigMap watch is an endless loop.
			for i := 0; i < 5; i++ {
				reconcileBoth()
				again := getParamsConfigMap(t, cl, cr.Namespace)
				assert.Equal(t, tt.wantData, again.Data, "pass %d changed the data", i+2)
				assert.Equal(t, first.ResourceVersion, again.ResourceVersion,
					"pass %d issued a write; the two reconcilers are fighting over the ConfigMap", i+2)
			}
		})
	}
}
