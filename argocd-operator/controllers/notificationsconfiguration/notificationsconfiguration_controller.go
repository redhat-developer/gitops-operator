/*
Copyright 2019, 2021.

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

package notificationsconfiguration

import (
	"context"
	"fmt"

	v1alpha1 "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logr "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// blank assignment to verify that NotificationsConfigurationReconciler implements reconcile.Reconciler
var _ reconcile.Reconciler = &NotificationsConfigurationReconciler{}

var log = logr.Log.WithName("controller_notificationsconfiguration")

// NotificationsConfigurationReconciler reconciles NotificationsConfiguration
// objects at the namespace level. Every event (CR or ConfigMap) in a namespace
// is mapped to a single reconcile key — the shared ConfigMap name — so the
// reconciler always operates on the full set of CRs in that namespace.
type NotificationsConfigurationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=*
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=*
// +kubebuilder:rbac:groups=argoproj.io,resources=notificationsconfiguration,verbs=*
func (r *NotificationsConfigurationReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {

	reqLogger := logr.FromContext(ctx, "Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling NotificationsConfiguration")

	if request.Namespace == "" {
		return reconcile.Result{}, fmt.Errorf("namespace-scoped reconciliation required but request has no namespace")
	}

	// List all NotificationsConfiguration CRs in the namespace.
	// A failed List must never be interpreted as "no CRs exist".
	crList := &v1alpha1.NotificationsConfigurationList{}
	if err := r.List(ctx, crList, client.InNamespace(request.Namespace)); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to list NotificationsConfigurations in namespace %s: %w", request.Namespace, err)
	}

	// Count active (non-deleting) CRs to decide create-vs-delete.
	activeCRs := 0
	for i := range crList.Items {
		if crList.Items[i].DeletionTimestamp == nil {
			activeCRs++
		}
	}

	// Fetch the existing ConfigMap.
	existingCM := &corev1.ConfigMap{}
	cmKey := types.NamespacedName{
		Namespace: request.Namespace,
		Name:      ArgoCDNotificationsConfigMap,
	}
	cmExists := true
	if err := r.Get(ctx, cmKey, existingCM); err != nil {
		if !errors.IsNotFound(err) {
			return reconcile.Result{}, fmt.Errorf("failed to get ConfigMap %s: %w", cmKey, err)
		}
		cmExists = false
	}

	// --- No active CRs → cleanup ---
	if activeCRs == 0 {
		if cmExists {
			return reconcile.Result{}, r.deleteManagedConfigMap(ctx, existingCM)
		}
		return reconcile.Result{}, nil
	}

	// --- Active CRs exist → merge ---
	mergeResult, err := MergeNotificationsConfigurations(crList.Items)
	if err != nil {
		reqLogger.Error(err, "merge failed, preserving existing ConfigMap data")
		return reconcile.Result{}, err
	}

	for _, skipped := range mergeResult.SkippedCRs {
		reqLogger.Error(nil,
			"CR skipped: invalid configuration, all entries from this CR are excluded from the merged ConfigMap",
			"cr", fmt.Sprintf("%s/%s", request.Namespace, skipped.Name),
			"reason", skipped.Reason,
		)
	}

	for _, conflict := range mergeResult.Conflicts {
		reqLogger.Info(
			"Warning: merge conflict: selected CR takes precedence by oldest creationTimestamp, "+
				"then alphabetical CR name; conflicting entry ignored",
			"key", conflict.Key,
			"selectedCR", fmt.Sprintf("%s/%s", request.Namespace, conflict.ExistingCR),
			"conflictingCR", fmt.Sprintf("%s/%s", request.Namespace, conflict.ConflictingCR),
		)
	}

	for _, dup := range mergeResult.Duplicates {
		reqLogger.Info("duplicate key: same value defined in multiple NotificationsConfiguration CRs",
			"key", dup.Key,
			"firstCR", fmt.Sprintf("%s/%s", request.Namespace, dup.FirstCR),
			"duplicateCR", fmt.Sprintf("%s/%s", request.Namespace, dup.DuplicateCR),
		)
	}

	// --- Create or update ---
	if !cmExists {
		return reconcile.Result{}, r.createManagedConfigMap(ctx, request.Namespace, mergeResult.Data)
	}

	if existingCM.DeletionTimestamp != nil {
		reqLogger.Info("ConfigMap is being deleted, requeueing to create a fresh one later")
		return reconcile.Result{Requeue: true}, nil
	}

	return reconcile.Result{}, r.updateManagedConfigMap(ctx, existingCM, mergeResult.Data)
}

// SetupWithManager registers namespace-level watches for
// NotificationsConfiguration CRs and the target ConfigMap.
func (r *NotificationsConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("notificationsconfiguration").
		Watches(
			&v1alpha1.NotificationsConfiguration{},
			handler.EnqueueRequestsFromMapFunc(mapCRToConfigMapRequest),
		).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(mapConfigMapToReconcileRequest),
		).
		Complete(r)
}

// mapCRToConfigMapRequest maps any NotificationsConfiguration event to a
// reconcile request keyed by the shared ConfigMap in the CR's namespace.
func mapCRToConfigMapRequest(_ context.Context, obj client.Object) []reconcile.Request {
	if obj.GetNamespace() == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      ArgoCDNotificationsConfigMap,
		},
	}}
}

// mapConfigMapToReconcileRequest maps ConfigMap events to reconcile requests,
// but only for ConfigMaps named argocd-notifications-cm. Events for unrelated
// ConfigMaps produce no requests.
func mapConfigMapToReconcileRequest(_ context.Context, obj client.Object) []reconcile.Request {
	if obj.GetName() != ArgoCDNotificationsConfigMap {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      ArgoCDNotificationsConfigMap,
		},
	}}
}
