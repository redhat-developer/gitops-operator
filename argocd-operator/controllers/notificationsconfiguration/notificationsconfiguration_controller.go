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
	"time"

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

var _ reconcile.Reconciler = &NotificationsConfigurationReconciler{}

var log = logr.Log.WithName("controller_notificationsconfiguration")

// NotificationsConfigurationReconciler merges a namespace's CRs into one ConfigMap.
// CR and ConfigMap events share one reconcile key per namespace.
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

	// A failed List must not trigger cleanup as though no CRs exist.
	crList := &v1alpha1.NotificationsConfigurationList{}
	if err := r.List(ctx, crList, client.InNamespace(request.Namespace)); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to list NotificationsConfigurations in namespace %s: %w", request.Namespace, err)
	}

	// Deleting CRs no longer contribute to the shared ConfigMap.
	activeCRs := 0
	for i := range crList.Items {
		if crList.Items[i].DeletionTimestamp == nil {
			activeCRs++
		}
	}

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

	if activeCRs == 0 {
		if cmExists {
			return reconcile.Result{}, r.deleteManagedConfigMap(ctx, existingCM)
		}
		return reconcile.Result{}, nil
	}

	mergeResult, err := MergeNotificationsConfigurations(crList.Items, existingCM.Data)
	if err != nil {
		reqLogger.Error(err, "merge failed, preserving existing ConfigMap data")
		return reconcile.Result{}, err
	}

	if mergeResult.InvalidExistingContext {
		reqLogger.Error(nil,
			"existing context is not a single YAML string mapping; disputed context entries omitted, other entries merged from CRs",
			"key", contextConfigMapKey,
			"configMap", cmKey.String(),
		)
	}

	for _, skipped := range mergeResult.SkippedEntries {
		reqLogger.Error(nil,
			"entry skipped: invalid key excluded from the merged ConfigMap; other entries from this CR are still merged",
			"key", skipped.Key,
			"cr", fmt.Sprintf("%s/%s", request.Namespace, skipped.CR),
			"reason", skipped.Reason,
		)
	}

	for _, conflict := range mergeResult.Conflicts {
		if conflict.EqualTimestamp {
			reqLogger.Error(nil,
				"merge conflict: equal creation timestamps; existing value retained only if an oldest tied CR defines it, otherwise key omitted",
				"key", conflict.Key,
				"existingCR", fmt.Sprintf("%s/%s", request.Namespace, conflict.ExistingCR),
				"conflictingCR", fmt.Sprintf("%s/%s", request.Namespace, conflict.ConflictingCR),
			)
			continue
		}
		reqLogger.Info(
			"Warning: merge conflict: newer CR entry ignored",
			"key", conflict.Key,
			"olderCR", fmt.Sprintf("%s/%s", request.Namespace, conflict.ExistingCR),
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

	if !cmExists {
		return reconcile.Result{}, r.createManagedConfigMap(ctx, request.Namespace, mergeResult.Data)
	}

	if existingCM.DeletionTimestamp != nil {
		reqLogger.Info("ConfigMap is being deleted, requeueing to create a fresh one later")
		return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
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

// mapConfigMapToReconcileRequest queues only events for the shared notifications ConfigMap.
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
