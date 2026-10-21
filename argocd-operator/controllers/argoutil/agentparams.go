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
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	argoproj "github.com/argoproj-labs/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/argoproj-labs/gitops-operator/argocd-operator/common"
)

// AgentParamsConfigMapOptions describes the agent/principal's details required to reconcile the
// argocd-agent-params ConfigMap.
//
// The principal and the agent each reconcile a ConfigMap of this name in their own namespace.
// In a real topology they never meet, because the principal runs on the control plane cluster
// and the agent on a workload cluster. Conflict is prevented since only one of agent/principal can be enabled
// at a time.
type AgentParamsConfigMapOptions struct {
	// Component identifies the owning component, matching the app.kubernetes.io/component
	// label: "principal" or "agent".
	Component string

	// Labels is stamped onto a ConfigMap this component creates. It must carry
	// common.ArgoCDKeyComponent set to Component, or the ConfigMap will not be recognised as
	// this component's on the next reconcile.
	Labels map[string]string

	// ManagedKeys are the ConfigMap keys this component is authoritative for. Keys outside this
	// set are never touched, so a newer upstream setting the operator does not know about is
	// not destroyed on the next reconcile.
	ManagedKeys []string

	// Desired holds the value for every managed key.
	Desired map[string]string

	// Enabled reports whether this component is enabled on the ArgoCD CR.
	Enabled bool
}

// ReconcileAgentParamsConfigMap manages the agent/principal's keys in the argocd-agent-params
// ConfigMap, which the agent/principal watch for settings they apply at runtime.
func ReconcileAgentParamsConfigMap(c client.Client, log logr.Logger, cr *argoproj.ArgoCD, scheme *runtime.Scheme, opts AgentParamsConfigMapOptions) error {
	name := common.ArgoCDAgentParamsConfigMapName

	if opts.Labels[common.ArgoCDKeyComponent] != opts.Component {
		return fmt.Errorf("cannot reconcile %s configmap for component %q: missing label %s=%s",
			name, opts.Component, common.ArgoCDKeyComponent, opts.Component,
		)
	}

	existing := &corev1.ConfigMap{}
	exists := true
	if err := c.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: cr.Namespace}, existing); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get existing %s configmap in namespace %s: %w", name, cr.Namespace, err)
		}
		exists = false
	}

	// ours is false when the ConfigMap belongs to the sibling component, or to an admin who
	// created it by hand before the operator managed it. In both cases the operator still manages its own
	// keys inside it, but must never adopt or delete the object itself.
	ours := exists && isOwnedByAgentComponent(existing, cr, opts.Component)

	if !opts.Enabled {
		if !exists {
			return nil
		}
		if ours {
			LogResourceDeletion(log, existing, fmt.Sprintf("%s params configmap is being deleted as the component is disabled", opts.Component))
			if err := c.Delete(context.TODO(), existing); err != nil {
				return fmt.Errorf("failed to delete %s configmap: %w", name, err)
			}
			return nil
		}
		// Fall through with an empty Desired so that only this component's own keys are
		// stripped, leaving the sibling's keys and the object itself in place.
	}

	if !exists {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: cr.Namespace,
				Labels:    opts.Labels,
			},
			Data: opts.Desired,
		}
		if err := controllerutil.SetControllerReference(cr, cm, scheme); err != nil {
			return fmt.Errorf("failed to set ArgoCD CR %s as owner for configmap %s: %w", cr.Name, name, err)
		}
		LogResourceCreation(log, cm)
		if err := c.Create(context.TODO(), cm); err != nil {
			return fmt.Errorf("failed to create %s configmap: %w", name, err)
		}
		return nil
	}

	data := existing.Data
	if data == nil {
		data = map[string]string{}
	}

	changed := false
	for _, key := range opts.ManagedKeys {
		want, set := opts.Desired[key]
		current, present := data[key]
		switch {
		case opts.Enabled && set && (!present || current != want):
			data[key] = want
			changed = true
		case (!opts.Enabled || !set) && present:
			delete(data, key)
			changed = true
		}
	}

	if !changed {
		return nil
	}

	existing.Data = data
	LogResourceUpdate(log, existing, fmt.Sprintf("%s params are being updated", opts.Component))
	if err := c.Update(context.TODO(), existing); err != nil {
		return fmt.Errorf("failed to update %s configmap: %w", name, err)
	}
	return nil
}

// isOwnedByAgentComponent reports whether the ConfigMap was created by this operator for this
// particular Argo CD Agent component.
func isOwnedByAgentComponent(cm *corev1.ConfigMap, cr *argoproj.ArgoCD, component string) bool {
	ref := metav1.GetControllerOf(cm)
	if ref == nil || ref.Kind != "ArgoCD" || ref.Name != cr.Name {
		return false
	}
	return cm.Labels[common.ArgoCDKeyComponent] == component
}

// AgentFullDetailParam returns the value to write for a full-detail key. An empty value turns
// full-detail logging off, which is also the default.
func AgentFullDetailParam(categories []string) string {
	return strings.Join(categories, ",")
}

// AgentPProfEnabledParam returns the value to write for a pprof key. Unset means disabled.
//
// Never empty: argocd-agent runs strconv.ParseBool on this key and logs a warning for a value
// it cannot parse, which an empty string would trigger on every single reconcile.
func AgentPProfEnabledParam(enabled *bool) string {
	return strconv.FormatBool(enabled != nil && *enabled)
}

// AgentParamsEnvVarSource returns an env var source reading the given key from the
// argocd-agent-params ConfigMap.
func AgentParamsEnvVarSource(key string) *corev1.EnvVarSource {
	return &corev1.EnvVarSource{
		ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: common.ArgoCDAgentParamsConfigMapName,
			},
			Key:      key,
			Optional: new(true),
		},
	}
}
