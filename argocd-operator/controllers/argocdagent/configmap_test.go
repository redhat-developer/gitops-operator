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
)

func withPrincipalLogLevel(level string) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		withPrincipalEnabled(true)(a)
		a.Spec.ArgoCDAgent.Principal.LogLevel = level
	}
}

func withPrincipalLogFullDetail(categories ...string) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		withPrincipalEnabled(true)(a)
		a.Spec.ArgoCDAgent.Principal.LogFullDetail = categories
	}
}

func withPrincipalPProf(pprof *argoproj.ArgoCDAgentPProfSpec) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		withPrincipalEnabled(true)(a)
		a.Spec.ArgoCDAgent.Principal.PProf = pprof
	}
}

// getParamsConfigMap fetches the params ConfigMap, failing the test if it is absent.
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

func TestReconcilePrincipalParamsConfigMap_NeitherComponentEnabled_IsNotCreated(t *testing.T) {
	cr := makeTestArgoCD(withPrincipalEnabled(false))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	cm := &corev1.ConfigMap{}
	err := cl.Get(context.TODO(), types.NamespacedName{
		Name:      common.ArgoCDAgentParamsConfigMapName,
		Namespace: cr.Namespace,
	}, cm)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcilePrincipalParamsConfigMap_NoAgentSpecAtAll(t *testing.T) {
	// A CR that has never mentioned the agent must not get a params ConfigMap.
	cr := makeTestArgoCD()
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	cm := &corev1.ConfigMap{}
	err := cl.Get(context.TODO(), types.NamespacedName{
		Name:      common.ArgoCDAgentParamsConfigMapName,
		Namespace: cr.Namespace,
	}, cm)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcilePrincipalParamsConfigMap_PrincipalEnabled_NoDebugFieldsSet(t *testing.T) {
	// The ConfigMap is created so the knobs have a home, but it carries no managed keys:
	// writing defaults would shadow anything supplied through the principal's `env` field.
	cr := makeTestArgoCD(withPrincipalEnabled(true))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	cm := getParamsConfigMap(t, cl, cr.Namespace)
	// Every managed key carries the principal's own default rather than being left out, so the
	// deployment can safely source them with a ConfigMapKeyRef.
	assert.Equal(t, map[string]string{
		common.ArgoCDAgentPrincipalLogLevelKey:     "",
		common.ArgoCDAgentPrincipalFullDetailKey:   "",
		common.ArgoCDAgentPrincipalPProfEnabledKey: "false",
	}, cm.Data)

	// Owned by the CR, so it is garbage collected with the CR and watched by the controller.
	owner := metav1.GetControllerOf(cm)
	require.NotNil(t, owner)
	assert.Equal(t, "ArgoCD", owner.Kind)
	assert.Equal(t, cr.Name, owner.Name)

	assert.Equal(t, "argocd-agent", cm.Labels[common.ArgoCDKeyPartOf])
	assert.Equal(t, cr.Name, cm.Labels[common.ArgoCDKeyManagedBy])
	assert.Equal(t, testCompName, cm.Labels[common.ArgoCDKeyComponent])
}

func TestReconcilePrincipalParamsConfigMap_PrincipalFieldsAreWritten(t *testing.T) {
	cr := makeTestArgoCD(
		withPrincipalLogLevel("debug"),
		withPrincipalLogFullDetail("events", "informers"),
		withPrincipalPProf(&argoproj.ArgoCDAgentPProfSpec{Enabled: new(true)}),
	)
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	cm := getParamsConfigMap(t, cl, cr.Namespace)
	assert.Equal(t, map[string]string{
		common.ArgoCDAgentPrincipalLogLevelKey:     "debug",
		common.ArgoCDAgentPrincipalFullDetailKey:   "events,informers",
		common.ArgoCDAgentPrincipalPProfEnabledKey: "true",
	}, cm.Data)
}

func TestReconcilePrincipalParamsConfigMap_DisabledComponentKeysAreNotWritten(t *testing.T) {
	// The principal is enabled, so none of the agent-prefixed keys may appear even though the
	// agent sub-spec carries values.
	cr := makeTestArgoCD(withPrincipalLogLevel("debug"))
	cr.Spec.ArgoCDAgent.Agent = &argoproj.AgentSpec{
		Enabled:  new(false),
		LogLevel: "trace",
		PProf:    &argoproj.ArgoCDAgentPProfSpec{Enabled: new(true)},
	}

	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	cm := getParamsConfigMap(t, cl, cr.Namespace)
	assert.Equal(t, map[string]string{
		common.ArgoCDAgentPrincipalLogLevelKey:     "debug",
		common.ArgoCDAgentPrincipalFullDetailKey:   "",
		common.ArgoCDAgentPrincipalPProfEnabledKey: "false",
	}, cm.Data)
}

func TestReconcilePrincipalParamsConfigMap_EmptyFullDetailMeansOff(t *testing.T) {
	cr := makeTestArgoCD(withPrincipalLogFullDetail())
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	assert.Equal(t, "", getParamsConfigMap(t, cl, cr.Namespace).Data[common.ArgoCDAgentPrincipalFullDetailKey])
}

func TestReconcilePrincipalParamsConfigMap_ClearingFullDetailTurnsItOff(t *testing.T) {
	cr := makeTestArgoCD(withPrincipalLogFullDetail("events"))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))
	assert.Equal(t, "events", getParamsConfigMap(t, cl, cr.Namespace).Data[common.ArgoCDAgentPrincipalFullDetailKey])

	cr.Spec.ArgoCDAgent.Principal.LogFullDetail = nil
	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	// Reset to empty rather than removed
	assert.Equal(t, "", getParamsConfigMap(t, cl, cr.Namespace).Data[common.ArgoCDAgentPrincipalFullDetailKey])
}

func TestReconcilePrincipalParamsConfigMap_UpdatesChangedValue(t *testing.T) {
	cr := makeTestArgoCD(withPrincipalLogLevel("debug"))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))
	assert.Equal(t, "debug", getParamsConfigMap(t, cl, cr.Namespace).Data[common.ArgoCDAgentPrincipalLogLevelKey])

	cr.Spec.ArgoCDAgent.Principal.LogLevel = "trace"
	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))
	assert.Equal(t, "trace", getParamsConfigMap(t, cl, cr.Namespace).Data[common.ArgoCDAgentPrincipalLogLevelKey])
}

func TestReconcilePrincipalParamsConfigMap_UnsettingAFieldRevertsToTheDefault(t *testing.T) {
	cr := makeTestArgoCD(withPrincipalPProf(&argoproj.ArgoCDAgentPProfSpec{Enabled: new(true)}))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))
	assert.Equal(t, "true", getParamsConfigMap(t, cl, cr.Namespace).Data[common.ArgoCDAgentPrincipalPProfEnabledKey])

	cr.Spec.ArgoCDAgent.Principal.PProf = nil
	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	// Written as "false", not removed. Removing it would let a pod that restarted while pprof
	// was on fall back to its env baseline and keep the endpoint open forever.
	assert.Equal(t, "false", getParamsConfigMap(t, cl, cr.Namespace).Data[common.ArgoCDAgentPrincipalPProfEnabledKey])
}

func TestReconcilePrincipalParamsConfigMap_RevertsManualEditOfAManagedKey(t *testing.T) {
	// The CR is the source of truth for the managed keys, so a hand-edit is reverted.
	cr := makeTestArgoCD(withPrincipalLogLevel("debug"))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	cm := getParamsConfigMap(t, cl, cr.Namespace)
	cm.Data[common.ArgoCDAgentPrincipalLogLevelKey] = "trace"
	require.NoError(t, cl.Update(context.TODO(), cm))

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))
	assert.Equal(t, "debug", getParamsConfigMap(t, cl, cr.Namespace).Data[common.ArgoCDAgentPrincipalLogLevelKey])
}

func TestReconcilePrincipalParamsConfigMap_PreservesUnmanagedKeys(t *testing.T) {
	// A key the operator does not know about (a newer upstream setting, or one an admin added)
	// must survive, otherwise every reconcile would destroy it.
	cr := makeTestArgoCD(withPrincipalLogLevel("debug"))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	cm := getParamsConfigMap(t, cl, cr.Namespace)
	cm.Data["principal.some.future.setting"] = "42"
	require.NoError(t, cl.Update(context.TODO(), cm))

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	cm = getParamsConfigMap(t, cl, cr.Namespace)
	assert.Equal(t, "42", cm.Data["principal.some.future.setting"])
	assert.Equal(t, "debug", cm.Data[common.ArgoCDAgentPrincipalLogLevelKey])
}

func TestReconcilePrincipalParamsConfigMap_ManagesKeysInAnUnownedConfigMapWithoutAdoptingIt(t *testing.T) {
	// An admin may have created the ConfigMap by hand before the operator managed it - the name
	// is the upstream default. The operator takes over its own keys but must NOT adopt the
	// object: an ownerRef would make the admin's other keys disappear with the ArgoCD CR.
	cr := makeTestArgoCD(withPrincipalLogLevel("debug"))
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.ArgoCDAgentParamsConfigMapName,
			Namespace: cr.Namespace,
		},
		Data: map[string]string{
			common.ArgoCDAgentPrincipalLogLevelKey: "trace",
			"principal.pprof.port":                 "6060",
		},
	}

	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr, existing})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	cm := getParamsConfigMap(t, cl, cr.Namespace)
	assert.Equal(t, "debug", cm.Data[common.ArgoCDAgentPrincipalLogLevelKey])
	assert.Equal(t, "6060", cm.Data["principal.pprof.port"])
	assert.Nil(t, metav1.GetControllerOf(cm), "the operator must not adopt a ConfigMap it did not create")
}

func TestReconcilePrincipalParamsConfigMap_DeletedWhenComponentsAreDisabled(t *testing.T) {
	cr := makeTestArgoCD(withPrincipalLogLevel("debug"))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))
	getParamsConfigMap(t, cl, cr.Namespace)

	withPrincipalEnabled(false)(cr)
	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	cm := &corev1.ConfigMap{}
	err := cl.Get(context.TODO(), types.NamespacedName{
		Name:      common.ArgoCDAgentParamsConfigMapName,
		Namespace: cr.Namespace,
	}, cm)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcilePrincipalParamsConfigMap_DeletedWhenTheAgentSpecIsRemovedEntirely(t *testing.T) {
	// Dropping spec.argoCDAgent altogether is a different nil path than setting enabled: false,
	// and the reconciler runs unconditionally, so it has to clean up in both cases.
	cr := makeTestArgoCD(withPrincipalLogLevel("debug"))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))
	getParamsConfigMap(t, cl, cr.Namespace)

	cr.Spec.ArgoCDAgent = nil
	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	cm := &corev1.ConfigMap{}
	err := cl.Get(context.TODO(), types.NamespacedName{
		Name:      common.ArgoCDAgentParamsConfigMapName,
		Namespace: cr.Namespace,
	}, cm)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcilePrincipalParamsConfigMap_DoesNotDeleteAConfigMapItDoesNotOwn(t *testing.T) {
	// Disabling the component strips the operator's own keys, but a ConfigMap the operator
	// never created is not its to delete.
	cr := makeTestArgoCD(withPrincipalEnabled(false))
	foreign := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.ArgoCDAgentParamsConfigMapName,
			Namespace: cr.Namespace,
		},
		Data: map[string]string{
			common.ArgoCDAgentPrincipalLogLevelKey: "trace",
			"principal.pprof.port":                 "6060",
		},
	}

	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr, foreign})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	cm := getParamsConfigMap(t, cl, cr.Namespace)
	assert.Equal(t, "6060", cm.Data["principal.pprof.port"], "unmanaged keys must survive")
	_, present := cm.Data[common.ArgoCDAgentPrincipalLogLevelKey]
	assert.False(t, present, "the operator's own keys are stripped once the component is disabled")
}

func TestReconcilePrincipalParamsConfigMap_IsIdempotent(t *testing.T) {
	cr := makeTestArgoCD(
		withPrincipalLogLevel("debug"),
		withPrincipalLogFullDetail("events"),
		withPrincipalPProf(&argoproj.ArgoCDAgentPProfSpec{Enabled: new(true)}),
	)
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))
	first := getParamsConfigMap(t, cl, cr.Namespace)

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))
	second := getParamsConfigMap(t, cl, cr.Namespace)

	assert.Equal(t, first.Data, second.Data)
	// A no-op reconcile must not issue an Update, or the controller would hot-loop against its
	// own ConfigMap watch.
	assert.Equal(t, first.ResourceVersion, second.ResourceVersion)
}

func TestBuildPrincipalParams_WritesEveryKey(t *testing.T) {
	// Enabled with nothing configured still yields every key, at the principal's own defaults.
	cr := makeTestArgoCD(withPrincipalEnabled(true))
	assert.Equal(t, map[string]string{
		common.ArgoCDAgentPrincipalLogLevelKey:     "",
		common.ArgoCDAgentPrincipalFullDetailKey:   "",
		common.ArgoCDAgentPrincipalPProfEnabledKey: "false",
	}, buildPrincipalParams(cr))

	// Disabled yields nothing, so the keys are stripped.
	assert.Empty(t, buildPrincipalParams(makeTestArgoCD(withPrincipalEnabled(false))))
}

func TestReconcilePrincipalParamsConfigMap_DoesNotTouchTheAgentsConfigMap(t *testing.T) {
	// Mirror of the agent-side regression test. Both reconcilers target a ConfigMap of the same
	// name in the same namespace on every reconcile, so whichever component is disabled must
	// not delete the other's ConfigMap - that would be a delete/create loop driven by the
	// operator's own ConfigMap watch.
	cr := makeTestArgoCD(withPrincipalEnabled(false))
	cr.Spec.ArgoCDAgent.Agent = &argoproj.AgentSpec{Enabled: new(true), LogLevel: "trace"}

	agentCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.ArgoCDAgentParamsConfigMapName,
			Namespace: cr.Namespace,
			Labels:    map[string]string{common.ArgoCDKeyComponent: "agent"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: argoproj.GroupVersion.String(),
				Kind:       "ArgoCD",
				Name:       cr.Name,
				UID:        cr.UID,
				Controller: new(true),
			}},
		},
		Data: map[string]string{common.ArgoCDAgentAgentLogLevelKey: "trace"},
	}

	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr, agentCM})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	cm := getParamsConfigMap(t, cl, cr.Namespace)
	assert.Equal(t, "trace", cm.Data[common.ArgoCDAgentAgentLogLevelKey],
		"the principal reconciler must leave the agent's ConfigMap and keys alone")
	assert.Equal(t, "agent", cm.Labels[common.ArgoCDKeyComponent])
}

func TestReconcilePrincipalParamsConfigMap_CarriesTheComponentLabel(t *testing.T) {
	cr := makeTestArgoCD(withPrincipalLogLevel("debug"))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))

	cm := getParamsConfigMap(t, cl, cr.Namespace)
	assert.Equal(t, testCompName, cm.Labels[common.ArgoCDKeyComponent],
		"the component label is what keeps the agent reconciler from deleting this ConfigMap")
}

func TestPrincipalParamsKeys_AreAllWrittenWhileEnabled(t *testing.T) {
	// This is the invariant the ConfigMapKeyRef in the deployment depends on: while the
	// principal is enabled every managed key must have a value, whatever the CR says. If any
	// key could go missing, a pod that restarted while it was set would adopt that value as its
	// baseline and never let go of it.
	for name, cr := range map[string]*argoproj.ArgoCD{
		"nothing configured": makeTestArgoCD(withPrincipalEnabled(true)),
		"log level only":     makeTestArgoCD(withPrincipalLogLevel("debug")),
		"unusual log level":  makeTestArgoCD(withPrincipalLogLevel("resource-proxy=trace")),
		"full detail only":   makeTestArgoCD(withPrincipalLogFullDetail("events")),
		"pprof only":         makeTestArgoCD(withPrincipalPProf(&argoproj.ArgoCDAgentPProfSpec{Enabled: new(true)})),
		"pprof explicit off": makeTestArgoCD(withPrincipalPProf(&argoproj.ArgoCDAgentPProfSpec{Enabled: new(false)})),
	} {
		params := buildPrincipalParams(cr)
		for _, key := range principalParamsKeys {
			_, present := params[key]
			assert.True(t, present, "%s: key %s must always be written while the principal is enabled", name, key)
		}
		assert.Len(t, params, len(principalParamsKeys), "%s: no stray keys", name)
	}
}

func TestReconcilePrincipalParamsConfigMap_WritesTheLogLevelVerbatim(t *testing.T) {
	for _, value := range []string{"debug", "resource-proxy=trace", "some-new-subsystem=trace"} {
		cr := makeTestArgoCD(withPrincipalLogLevel(value))
		sch := makeTestReconcilerScheme()
		cl := makeTestReconcilerClient(sch, []client.Object{cr})

		require.NoError(t, ReconcilePrincipalParamsConfigMap(cl, testCompName, cr, sch))
		assert.Equal(t, value,
			getParamsConfigMap(t, cl, cr.Namespace).Data[common.ArgoCDAgentPrincipalLogLevelKey])
	}
}
