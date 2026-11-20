package argocd

import (
	"context"
	"errors"
	"reflect"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/api/v1alpha1"
	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
	aiconfig "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/ai"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	aiNotificationsManagedAnnotation         = "gitops.openshift.io/ai-notifications-managed"
	aiNotificationsControllerOwnerAnnotation = "gitops.openshift.io/ai-notifications-controller-owner"
)

func hasManagedAINotifications(cr *argoproj.ArgoCD) bool {
	return cr.UID != "" && cr.Annotations[aiNotificationsManagedAnnotation] == string(cr.UID)
}

// Older operators recorded ownership on the dedicated AI resources, but did
// not write the lifecycle annotations. Migrate before either provisioning or
// cleanup so disabling AI during an upgrade still revokes the old wiring.
func (r *ReconcileArgoCD) migrateLegacyAINotifications(cr *argoproj.ArgoCD) error {
	if cr.UID == "" || hasManagedAINotifications(cr) {
		return nil
	}
	nc := &unstructured.Unstructured{}
	nc.SetGroupVersionKind(v1alpha1.GroupVersion.WithKind("NotificationsConfiguration"))
	nc.SetName(aiNotificationsConfigurationName)
	evidence := []client.Object{
		nc,
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: aiNotificationsSAName}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: aiTriggerTokenSecretName}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: aiCAConfigMapName}},
	}
	legacy := false
	for _, obj := range evidence {
		obj.SetNamespace(cr.Namespace)
		if err := r.Get(context.TODO(), client.ObjectKeyFromObject(obj), obj); err != nil {
			if client.IgnoreNotFound(err) != nil && !meta.IsNoMatchError(err) {
				return err
			}
			continue
		}
		if aiNotificationOwnedBy(cr, obj) && obj.GetLabels()[aiconfig.EnrollmentManagedByLabel] == aiconfig.EnrollmentManager {
			legacy = true
			break
		}
	}
	if !legacy {
		return nil
	}
	if !isNotificationsEnabled(cr) {
		for _, obj := range aiOnlyNotificationsResources(cr) {
			if err := r.Get(context.TODO(), client.ObjectKeyFromObject(obj), obj); err != nil {
				if client.IgnoreNotFound(err) != nil {
					return err
				}
				continue
			}
			if obj.GetAnnotations()[aiNotificationsControllerOwnerAnnotation] != "" || !legacyNotificationsResourceOwnedBy(cr, obj) {
				continue
			}
			before := obj.DeepCopyObject().(client.Object)
			annotations := obj.GetAnnotations()
			if annotations == nil {
				annotations = map[string]string{}
			}
			annotations[aiNotificationsControllerOwnerAnnotation] = string(cr.UID)
			obj.SetAnnotations(annotations)
			if err := r.Patch(context.TODO(), obj, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
				return err
			}
		}
	}
	// Commit the instance marker only after backfilling succeeds. Until then,
	// retain the dedicated resources as evidence for a retry after a restart.
	return r.setAINotificationsManaged(cr, true)
}

func legacyNotificationsResourceOwnedBy(cr *argoproj.ArgoCD, obj client.Object) bool {
	// Legacy ownership does not prove that AI created shared credentials or
	// configuration. Preserve user data; cleanupAINotifications removes AI's token.
	switch obj.(type) {
	case *corev1.Secret, *v1alpha1.NotificationsConfiguration:
		return false
	}
	if obj.GetNamespace() != "" {
		return aiNotificationOwnedBy(cr, obj)
	}
	// Cluster-scoped RBAC cannot reference a namespaced owner. Require the
	// operator's instance metadata and expected notification permissions/binding,
	// in addition to the independently verified legacy AI evidence above.
	annotations, labels := obj.GetAnnotations(), obj.GetLabels()
	if annotations[common.AnnotationName] != cr.Name || annotations[common.AnnotationNamespace] != cr.Namespace ||
		labels[common.ArgoCDKeyManagedBy] != cr.Name || labels[common.ArgoCDKeyPartOf] != common.ArgoCDAppName {
		return false
	}
	component := common.ArgoCDNotificationsControllerComponent
	switch resource := obj.(type) {
	case *rbacv1.ClusterRole:
		return reflect.DeepEqual(resource.Rules, policyRuleForNotificationsControllerClusterRole())
	case *rbacv1.ClusterRoleBinding:
		return resource.RoleRef == (rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: GenerateUniqueResourceName(component, cr)}) &&
			reflect.DeepEqual(resource.Subjects, []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: nameWithSuffix(component, cr), Namespace: cr.Namespace}})
	default:
		return false
	}
}

// Persist enrollment intent before creating resources, and clear it only after
// cleanup completes. Ordinary instances never acquire a dependency on AI state,
// and retries still find partially cleaned instances after an operator restart.
func (r *ReconcileArgoCD) setAINotificationsManaged(cr *argoproj.ArgoCD, managed bool) error {
	if managed == hasManagedAINotifications(cr) {
		return nil
	}
	if cr.UID == "" {
		return errors.New("cannot manage AI notifications without an ArgoCD UID")
	}
	before := cr.DeepCopy()
	if managed {
		if cr.Annotations == nil {
			cr.Annotations = map[string]string{}
		}
		cr.Annotations[aiNotificationsManagedAnnotation] = string(cr.UID)
	} else {
		delete(cr.Annotations, aiNotificationsManagedAnnotation)
	}
	return r.Patch(context.TODO(), cr, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}

// Only resources newly created for AI alone are eligible for AI-only teardown.
// Existing user notification resources must survive removing the AI opt-in.
func markAIOnlyNotificationsResource(cr *argoproj.ArgoCD, obj client.Object) {
	if !AINotificationsEnabled(cr) || isNotificationsEnabled(cr) || cr.UID == "" {
		return
	}
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[aiNotificationsControllerOwnerAnnotation] = string(cr.UID)
	obj.SetAnnotations(annotations)
}

func aiOnlyNotificationsResources(cr *argoproj.ArgoCD) []client.Object {
	component := common.ArgoCDNotificationsControllerComponent
	meta := metav1.ObjectMeta{Name: nameWithSuffix(component, cr), Namespace: cr.Namespace}
	metricsMeta := metav1.ObjectMeta{Name: nameWithSuffix("notifications-controller-metrics", cr), Namespace: cr.Namespace}
	clusterMeta := metav1.ObjectMeta{Name: GenerateUniqueResourceName(component, cr)}
	objects := []client.Object{
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: nameWithSuffix("notifications-controller", cr), Namespace: cr.Namespace}},
		&rbacv1.RoleBinding{ObjectMeta: meta},
		&rbacv1.ClusterRoleBinding{ObjectMeta: clusterMeta},
		&rbacv1.Role{ObjectMeta: meta},
		&rbacv1.ClusterRole{ObjectMeta: clusterMeta},
		&corev1.ServiceAccount{ObjectMeta: meta},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: aiNotificationsSecretName, Namespace: cr.Namespace}},
		&v1alpha1.NotificationsConfiguration{ObjectMeta: metav1.ObjectMeta{Name: DefaultNotificationsConfigurationInstanceName, Namespace: cr.Namespace}},
		&corev1.Service{ObjectMeta: metricsMeta},
	}
	if IsPrometheusAPIAvailable() {
		objects = append(objects, &monitoringv1.ServiceMonitor{ObjectMeta: metricsMeta})
	}
	return objects
}

func (r *ReconcileArgoCD) cleanupAIOnlyNotificationsController(cr *argoproj.ArgoCD) error {
	if cr.UID == "" {
		return nil
	}
	for _, obj := range aiOnlyNotificationsResources(cr) {
		if err := r.Get(context.TODO(), client.ObjectKeyFromObject(obj), obj); err != nil {
			if err := client.IgnoreNotFound(err); err != nil {
				return err
			}
			continue
		}
		if obj.GetAnnotations()[aiNotificationsControllerOwnerAnnotation] != string(cr.UID) {
			continue
		}
		uid, version := obj.GetUID(), obj.GetResourceVersion()
		if err := client.IgnoreNotFound(r.Delete(context.TODO(), obj, client.Preconditions{UID: &uid, ResourceVersion: &version})); err != nil {
			return err
		}
	}
	return nil
}
