package notificationsconfiguration

import (
	"context"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argoutil"
)

const (
	ArgoCDNotificationsConfigMap = "argocd-notifications-cm"
)

// createManagedConfigMap creates the shared ConfigMap with an operator tracking
// label and no individual CR owner, so deleting one CR cannot garbage-collect it.
func (r *NotificationsConfigurationReconciler) createManagedConfigMap(ctx context.Context, namespace string, data map[string]string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ArgoCDNotificationsConfigMap,
			Namespace: namespace,
		},
		Data: data,
	}
	argoutil.AddTrackedByOperatorLabel(&cm.ObjectMeta)
	argoutil.LogResourceCreation(log, cm)
	return r.Create(ctx, cm)
}

// updateManagedConfigMap validates ownership before updating data and removing
// legacy CR owner references. It preserves other metadata and skips unchanged writes.
func (r *NotificationsConfigurationReconciler) updateManagedConfigMap(ctx context.Context, existing *corev1.ConfigMap, desiredData map[string]string) error {
	if !argoutil.IsTrackedByOperator(existing.Labels) {
		return fmt.Errorf(
			"ConfigMap %s/%s exists but is not managed by this operator "+
				"(missing or incorrect label %s); refusing to overwrite",
			existing.Namespace, existing.Name,
			common.ArgoCDTrackedByOperatorLabel,
		)
	}

	if hasUnsupportedOwnerReference(existing) {
		return fmt.Errorf(
			"ConfigMap %s/%s has an unsupported owner reference; "+
				"refusing to update until the ownership conflict is resolved",
			existing.Namespace, existing.Name,
		)
	}

	updated := existing.DeepCopy()

	changed := migrateLegacyOwnerReferences(updated)

	if !maps.Equal(updated.Data, desiredData) {
		updated.Data = desiredData
		changed = true
	}

	if !changed {
		return nil
	}

	argoutil.LogResourceUpdate(log, updated, "reconciling merged notifications configuration")
	return r.Update(ctx, updated)
}

// deleteManagedConfigMap deletes only operator-managed ConfigMaps with supported
// owners. UID/resource-version preconditions protect against concurrent changes;
// absent or already-deleting ConfigMaps need no further cleanup.
func (r *NotificationsConfigurationReconciler) deleteManagedConfigMap(ctx context.Context, cm *corev1.ConfigMap) error {
	if !argoutil.IsTrackedByOperator(cm.Labels) {
		log.Info("ConfigMap exists but is not managed by this operator, skipping deletion",
			"namespace", cm.Namespace, "name", cm.Name)
		return nil
	}

	if hasUnsupportedOwnerReference(cm) {
		return fmt.Errorf(
			"ConfigMap %s/%s has an unsupported owner reference; "+
				"refusing to delete until the ownership conflict is resolved",
			cm.Namespace, cm.Name,
		)
	}

	if cm.DeletionTimestamp != nil {
		return nil
	}

	uid := cm.UID
	rv := cm.ResourceVersion
	argoutil.LogResourceDeletion(log, cm)
	err := r.Delete(ctx, cm, client.Preconditions{
		UID:             &uid,
		ResourceVersion: &rv,
	})
	if errors.IsNotFound(err) {
		return nil
	}
	return err
}

// isRecognizedLegacyOwnerReference identifies NotificationsConfiguration
// controller references created by the old single-CR reconciler.
func isRecognizedLegacyOwnerReference(ref metav1.OwnerReference) bool {
	return ref.APIVersion == v1alpha1.GroupVersion.String() &&
		ref.Kind == "NotificationsConfiguration" &&
		ref.Name != "" &&
		ref.UID != "" &&
		ref.Controller != nil && *ref.Controller
}

// hasUnsupportedOwnerReference detects owners outside the legacy CR format,
// including non-controller references that could affect garbage collection.
func hasUnsupportedOwnerReference(cm *corev1.ConfigMap) bool {
	for _, ref := range cm.OwnerReferences {
		if !isRecognizedLegacyOwnerReference(ref) {
			return true
		}
	}
	return false
}

// migrateLegacyOwnerReferences removes individual CR ownership from the shared
// ConfigMap and reports whether it changed. Callers must first reject unsupported owners.
func migrateLegacyOwnerReferences(cm *corev1.ConfigMap) bool {
	if len(cm.OwnerReferences) == 0 {
		return false
	}

	for _, ref := range cm.OwnerReferences {
		log.Info("removing legacy owner reference",
			"namespace", cm.Namespace,
			"configmap", cm.Name,
			"owner", ref.Name,
			"ownerKind", ref.Kind,
			"ownerAPIVersion", ref.APIVersion,
		)
	}
	cm.OwnerReferences = nil
	return true
}
