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

// createManagedConfigMap creates a new operator-managed ConfigMap with the
// merged notification configuration data and the operator tracking label.
// No individual NotificationsConfiguration owner reference is set.
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

// updateManagedConfigMap updates an existing operator-managed ConfigMap to
// match the desired merged data. It also migrates legacy single-CR owner
// references left by the old reconciler.
//
// Returns nil without performing a write when the ConfigMap already matches
// the desired state and has no legacy owner references to remove.
//
// Preserves unrelated labels and annotations. Owner references are validated
// before any mutation: if the complete list contains any unsupported reference,
// the function returns an error without mutating the cached object or issuing
// an API write.
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

// deleteManagedConfigMap deletes the operator-managed ConfigMap. It:
//   - Leaves unmanaged ConfigMaps untouched.
//   - Refuses to delete when any unsupported owner reference is present.
//   - Treats a ConfigMap already marked for deletion as successful cleanup.
//   - Uses UID/ResourceVersion preconditions to avoid deleting a replacement.
//   - Treats NotFound as successful cleanup.
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

// isRecognizedLegacyOwnerReference returns true when the owner reference
// matches the complete shape produced by the old single-CR reconciler calling
// controllerutil.SetControllerReference:
//   - APIVersion: argoproj.io/v1alpha1
//   - Kind: NotificationsConfiguration
//   - Name: non-empty (any CR name, not just the default)
//   - UID: non-empty
//   - Controller: explicitly true
//
// A reference that partially matches (e.g. correct APIVersion/Kind but empty
// Name or missing Controller=true) is not recognized and will be treated as
// unsupported.
func isRecognizedLegacyOwnerReference(ref metav1.OwnerReference) bool {
	return ref.APIVersion == v1alpha1.GroupVersion.String() &&
		ref.Kind == "NotificationsConfiguration" &&
		ref.Name != "" &&
		ref.UID != "" &&
		ref.Controller != nil && *ref.Controller
}

// hasUnsupportedOwnerReference returns true if the ConfigMap carries any owner
// reference that is not a recognized legacy NotificationsConfiguration
// reference.
//
// This is a shared ConfigMap managed exclusively by the operator through its
// tracking label. Any owner reference that does not match the exact legacy
// SetControllerReference shape is unsupported, regardless of its Controller
// field value. Non-controller references from unknown sources are not
// automatically safe for a shared resource.
func hasUnsupportedOwnerReference(cm *corev1.ConfigMap) bool {
	for _, ref := range cm.OwnerReferences {
		if !isRecognizedLegacyOwnerReference(ref) {
			return true
		}
	}
	return false
}

// migrateLegacyOwnerReferences removes all owner references from the ConfigMap.
// The new multi-CR reconciler does not use individual CR ownership.
//
// Precondition: the caller must verify via hasUnsupportedOwnerReference that
// every reference is a recognized legacy shape before calling this function.
// This precondition is enforced by updateManagedConfigMap.
//
// Returns true if any references were removed.
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
