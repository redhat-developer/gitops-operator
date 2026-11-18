package argocd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/redhat-developer/gitops-operator/argocd-operator/api/v1alpha1"
	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argoutil"
	aiconfig "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/ai"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	aiNotifTriggerKey                = "trigger.on-gitops-ai-diagnose"
	aiNotifTemplateKey               = "template.gitops-ai-diagnose"
	aiNotifServiceKey                = "service.webhook.gitops-ai-controller"
	aiTriggerTokenSecretName         = "gitops-ai-controller-notifications-token" // #nosec G101 -- resource name, not a credential
	aiNotificationsSecretName        = "argocd-notifications-secret"              // #nosec G101 -- resource name, not a credential
	aiTriggerTokenKey                = "gitops-ai-controller-token"               // #nosec G101 -- Secret data key, not a credential
	aiTLSCertsConfigMapName          = "argocd-tls-certs-cm"
	aiCABundleKey                    = "service-ca.crt"
	aiCAConfigMapName                = "gitops-ai-controller-serving-ca"
	aiNotificationsSAName            = "gitops-ai-controller-notifications"
	aiTriggerServiceName             = aiconfig.ServiceName
	aiWebhookName                    = "gitops-ai-controller"
	aiTriggerTLSEnv                  = "GITOPS_AI_CONTROLLER_TLS_ENABLED"
	enableAIFeaturesEnv              = "GITOPS_AI_ENABLED"
	aiNotificationsConfigurationName = "gitops-ai-controller-notifications"
	aiCAInjectAnnotation             = "service.beta.openshift.io/inject-cabundle"
	aiNotificationsOwnerAnnotation   = "gitops.openshift.io/ai-notifications-owner"
)

// Missing dependencies are expected while the service-account and service-CA
// controllers populate their resources. The reconciler schedules a short retry.
var errAINotificationsPending = errors.New("waiting for AI notification configuration, token or serving CA")

// aiNotificationDependencyRequests routes configuration, token, CA, and enrollment changes to
// ArgoCD. A missing registry denies authorization until these reconciliations
// rebuild its entries from successfully configured instances.
func (r *ReconcileArgoCD) aiNotificationDependencyRequests(ctx context.Context, obj client.Object) []reconcile.Request {
	var enrollmentSelector labels.Selector
	listOptions := []client.ListOption{client.InNamespace(obj.GetNamespace())}
	switch obj.(type) {
	case *v1alpha1.NotificationsConfiguration:
		if obj.GetName() != aiNotificationsConfigurationName {
			return nil
		}
	case *corev1.Secret:
		if obj.GetName() != aiTriggerTokenSecretName && obj.GetName() != aiNotificationsSecretName {
			return nil
		}
	case *corev1.ConfigMap:
		if obj.GetName() == aiconfig.EnrollmentConfigMapName {
			operatorNamespace, err := argoutil.GetOperatorNamespace()
			if err != nil {
				log.Error(err, "unable to determine AI enrollment namespace")
				return nil
			}
			if obj.GetNamespace() != operatorNamespace {
				return nil
			}
			enrollmentSelector, err = labels.Parse(r.LabelSelector)
			if err != nil {
				log.Error(err, "unable to select ArgoCD instances for AI enrollment update")
				return nil
			}
			// The registry is shared across managed namespaces; a deletion event
			// cannot rely on its former data to discover which entries to restore.
			listOptions = []client.ListOption{client.MatchingLabelsSelector{Selector: enrollmentSelector}}
		} else if obj.GetName() != aiCAConfigMapName && obj.GetName() != aiTLSCertsConfigMapName {
			return nil
		}
	default:
		return nil
	}
	instances := &argoproj.ArgoCDList{}
	if err := r.List(ctx, instances, listOptions...); err != nil {
		log.Error(err, "unable to find ArgoCD instances for AI dependency update")
		return nil
	}
	requests := make([]reconcile.Request, 0, len(instances.Items))
	for _, instance := range instances.Items {
		if enrollmentSelector != nil && (instance.DeletionTimestamp != nil || !AINotificationsEnabled(&instance) || !enrollmentSelector.Matches(labels.Set(instance.Labels))) {
			continue
		}
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&instance)})
	}
	return requests
}

// AINotificationsEnabled returns true when the operator-level GITOPS_AI_ENABLED
// env var is set and the ArgoCD instance has opted in via .spec.ai.troubleshooting.
func AINotificationsEnabled(cr *argoproj.ArgoCD) bool {
	return strings.EqualFold(os.Getenv(enableAIFeaturesEnv), "true") &&
		cr.Spec.AI != nil && cr.Spec.AI.Troubleshooting
}

// reconcileAINotifications creates or updates the gitops-ai-controller-notifications
// NotificationsConfiguration CR with all AI-specific notification entries, then
// propagates the SA token and (if TLS is enabled) the serving CA into the
// ArgoCD notification secret and TLS certs configmap.
func (r *ReconcileArgoCD) reconcileAINotifications(cr *argoproj.ArgoCD) error {
	if !AINotificationsEnabled(cr) {
		return r.cleanupAINotifications(cr)
	}
	if cr.UID == "" {
		return errors.New("cannot manage AI notifications without an ArgoCD UID")
	}

	ns := cr.Namespace
	tlsEnabled := !strings.EqualFold(os.Getenv(aiTriggerTLSEnv), "false")

	scheme, port := "http", 8080
	if tlsEnabled {
		scheme, port = "https", 8443
	}
	triggerNS, err := argoutil.GetOperatorNamespace()
	if err != nil {
		return fmt.Errorf("could not determine operator namespace for gitops-ai-controller webhook URL: %w", err)
	}

	aiSpec := v1alpha1.NotificationsConfigurationSpec{
		Triggers:      map[string]string{aiNotifTriggerKey: aiconfig.NotificationTrigger()},
		Templates:     map[string]string{aiNotifTemplateKey: "webhook:\n  gitops-ai-controller:\n    method: POST\n    path: /api/v1/diagnose\n    body: |\n      {\"application\":\"{{.app.metadata.name}}\",\"applicationNamespace\":\"{{.app.metadata.namespace}}\"}\n"},
		Services:      map[string]string{aiNotifServiceKey: fmt.Sprintf("url: %s://%s.%s.svc:%d\nheaders:\n- name: Authorization\n  value: Bearer $gitops-ai-controller-token\n", scheme, aiTriggerServiceName, triggerNS, port)},
		Subscriptions: map[string]string{"subscriptions": "- recipients:\n  - gitops-ai-controller\n  triggers:\n  - on-gitops-ai-diagnose\n"},
	}

	aiNC := &v1alpha1.NotificationsConfiguration{}
	err = r.Get(context.TODO(), types.NamespacedName{Name: aiNotificationsConfigurationName, Namespace: ns}, aiNC)
	if apierrors.IsNotFound(err) {
		if err := r.Create(context.TODO(), &v1alpha1.NotificationsConfiguration{
			ObjectMeta: aiNotificationObjectMeta(cr, aiNotificationsConfigurationName),
			Spec:       aiSpec,
		}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		if err := r.reconcileAINotificationOwnership(cr, aiNC, nil); err != nil {
			return err
		}
		if !reflect.DeepEqual(aiNC.Spec, aiSpec) {
			before := aiNC.DeepCopy()
			aiNC.Spec = aiSpec
			if err := r.Patch(context.TODO(), aiNC, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
				return err
			}
		}
	}

	// Ensure the notifications SA exists in the ArgoCD namespace.
	sa := &corev1.ServiceAccount{}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: aiNotificationsSAName, Namespace: ns}, sa); apierrors.IsNotFound(err) {
		if err := r.Create(context.TODO(), &corev1.ServiceAccount{
			ObjectMeta: aiNotificationObjectMeta(cr, aiNotificationsSAName),
		}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if err := r.reconcileAINotificationOwnership(cr, sa, nil); err != nil {
		return err
	}

	// Ensure the SA token Secret exists so Kubernetes can populate it.
	tokenSec := &corev1.Secret{}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: aiTriggerTokenSecretName, Namespace: ns}, tokenSec); apierrors.IsNotFound(err) {
		meta := aiNotificationObjectMeta(cr, aiTriggerTokenSecretName)
		meta.Annotations = map[string]string{corev1.ServiceAccountNameKey: aiNotificationsSAName}
		if err := r.Create(context.TODO(), &corev1.Secret{
			ObjectMeta: meta,
			Type:       corev1.SecretTypeServiceAccountToken,
		}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if err := r.reconcileAINotificationOwnership(cr, tokenSec, map[string]string{corev1.ServiceAccountNameKey: aiNotificationsSAName}); err != nil {
		return err
	}

	// Ensure the serving CA ConfigMap exists (TLS only); OpenShift injects the CA bundle.
	if tlsEnabled {
		caMap := &corev1.ConfigMap{}
		if err := r.Get(context.TODO(), types.NamespacedName{Name: aiCAConfigMapName, Namespace: ns}, caMap); apierrors.IsNotFound(err) {
			meta := aiNotificationObjectMeta(cr, aiCAConfigMapName)
			meta.Annotations = map[string]string{aiCAInjectAnnotation: "true"}
			if err := r.Create(context.TODO(), &corev1.ConfigMap{
				ObjectMeta: meta,
			}); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if err := r.reconcileAINotificationOwnership(cr, caMap, map[string]string{aiCAInjectAnnotation: "true"}); err != nil {
			return err
		}
	}

	// Propagate the SA token into argocd-notifications-secret.
	token := &corev1.Secret{}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: aiTriggerTokenSecretName, Namespace: ns}, token); err != nil {
		if apierrors.IsNotFound(err) {
			return errAINotificationsPending
		}
		return err
	}
	if !aiNotificationOwnedBy(cr, token) {
		return fmt.Errorf("AI token Secret %s/%s is not owned by ArgoCD %s", ns, token.Name, cr.Name)
	}
	if len(token.Data["token"]) == 0 {
		return errAINotificationsPending
	}
	notifSecret := &corev1.Secret{}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: aiNotificationsSecretName, Namespace: ns}, notifSecret); err != nil {
		if apierrors.IsNotFound(err) {
			return errAINotificationsPending
		}
		return err
	}
	if string(notifSecret.Data[aiTriggerTokenKey]) != string(token.Data["token"]) || notifSecret.Annotations[aiNotificationsOwnerAnnotation] != string(cr.UID) {
		before := notifSecret.DeepCopy()
		if notifSecret.Data == nil {
			notifSecret.Data = map[string][]byte{}
		}
		notifSecret.Data[aiTriggerTokenKey] = token.Data["token"]
		if notifSecret.Annotations == nil {
			notifSecret.Annotations = map[string]string{}
		}
		notifSecret.Annotations[aiNotificationsOwnerAnnotation] = string(cr.UID)
		if err := r.Patch(context.TODO(), notifSecret, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}

	if !tlsEnabled {
		return r.reconcileAIEnrollment(cr, true)
	}

	// Propagate the serving CA into argocd-tls-certs-cm.
	ca := &corev1.ConfigMap{}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: aiCAConfigMapName, Namespace: ns}, ca); err != nil {
		if apierrors.IsNotFound(err) {
			return errAINotificationsPending
		}
		return err
	}
	if !aiNotificationOwnedBy(cr, ca) {
		return fmt.Errorf("AI CA ConfigMap %s/%s is not owned by ArgoCD %s", ns, ca.Name, cr.Name)
	}
	if ca.Data[aiCABundleKey] == "" {
		return errAINotificationsPending
	}
	certs := &corev1.ConfigMap{}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: aiTLSCertsConfigMapName, Namespace: ns}, certs); err != nil {
		if apierrors.IsNotFound(err) {
			return errAINotificationsPending
		}
		return err
	}
	host := fmt.Sprintf("%s.%s.svc", aiTriggerServiceName, triggerNS)
	if certs.Data[host] != ca.Data[aiCABundleKey] || certs.Annotations[aiNotificationsOwnerAnnotation] != string(cr.UID) {
		before := certs.DeepCopy()
		if certs.Data == nil {
			certs.Data = map[string]string{}
		}
		certs.Data[host] = ca.Data[aiCABundleKey]
		if certs.Annotations == nil {
			certs.Annotations = map[string]string{}
		}
		certs.Annotations[aiNotificationsOwnerAnnotation] = string(cr.UID)
		if err := r.Patch(context.TODO(), certs, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}
	return r.reconcileAIEnrollment(cr, true)
}

func (r *ReconcileArgoCD) cleanupAINotifications(cr *argoproj.ArgoCD) error {
	if cr.UID == "" {
		return nil // Without an identity, no existing resources belong to us.
	}
	ns := cr.Namespace
	// Revoke authorization before removing the notification wiring. A cleanup
	// retry for an old instance must not remove its successor's enrollment.
	if err := r.reconcileAIEnrollment(cr, false); err != nil {
		return err
	}

	// Delete the AI NC CR; the NC controller will rebuild argocd-notifications-cm
	// without AI entries on its next reconcile.
	if err := r.deleteAINotificationResource(cr, aiNotificationsConfigurationName, &v1alpha1.NotificationsConfiguration{}); err != nil {
		return err
	}

	// Remove the SA token from argocd-notifications-secret.
	secret := &corev1.Secret{}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: aiNotificationsSecretName, Namespace: ns}, secret); err == nil {
		if secret.Annotations[aiNotificationsOwnerAnnotation] == string(cr.UID) {
			before := secret.DeepCopy()
			delete(secret.Data, aiTriggerTokenKey)
			delete(secret.Annotations, aiNotificationsOwnerAnnotation)
			if err := r.Patch(context.TODO(), secret, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
				return err
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Remove the CA entry from argocd-tls-certs-cm.
	triggerNS, _ := argoutil.GetOperatorNamespace()
	if triggerNS == "" {
		triggerNS = ns
	}
	certs := &corev1.ConfigMap{}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: aiTLSCertsConfigMapName, Namespace: ns}, certs); err == nil {
		host := fmt.Sprintf("%s.%s.svc", aiTriggerServiceName, triggerNS)
		if certs.Annotations[aiNotificationsOwnerAnnotation] == string(cr.UID) {
			before := certs.DeepCopy()
			delete(certs.Data, host)
			delete(certs.Annotations, aiNotificationsOwnerAnnotation)
			if err := r.Patch(context.TODO(), certs, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
				return err
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Delete the token Secret, notifications SA, and CA ConfigMap.
	if err := r.deleteAINotificationResource(cr, aiTriggerTokenSecretName, &corev1.Secret{}); err != nil {
		return err
	}
	if err := r.deleteAINotificationResource(cr, aiNotificationsSAName, &corev1.ServiceAccount{}); err != nil {
		return err
	}
	if err := r.deleteAINotificationResource(cr, aiCAConfigMapName, &corev1.ConfigMap{}); err != nil {
		return err
	}

	return nil
}

// Each dedicated AI resource belongs to one incarnation of an ArgoCD instance.
// The controller reference also makes updates and deletions enqueue repair.
func aiNotificationObjectMeta(cr *argoproj.ArgoCD, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name: name, Namespace: cr.Namespace,
		Labels: map[string]string{aiconfig.EnrollmentManagedByLabel: aiconfig.EnrollmentManager},
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(cr, argoproj.GroupVersion.WithKind("ArgoCD")),
		},
	}
}

func aiNotificationOwnedBy(cr *argoproj.ArgoCD, obj client.Object) bool {
	owner := metav1.GetControllerOf(obj)
	return cr.UID != "" && obj.GetNamespace() == cr.Namespace && owner != nil &&
		owner.UID == cr.UID && owner.Name == cr.Name && owner.Kind == "ArgoCD" && owner.APIVersion == argoproj.GroupVersion.String()
}

// Do not adopt a name collision. Once ownership is verified, restore managed
// metadata without disturbing Kubernetes-injected annotations or data.
func (r *ReconcileArgoCD) reconcileAINotificationOwnership(cr *argoproj.ArgoCD, obj client.Object, annotations map[string]string) error {
	if !aiNotificationOwnedBy(cr, obj) {
		return fmt.Errorf("AI notification resource %T %s/%s is not owned by ArgoCD %s (%s)", obj, obj.GetNamespace(), obj.GetName(), cr.Name, cr.UID)
	}
	before := obj.DeepCopyObject().(client.Object)
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[aiconfig.EnrollmentManagedByLabel] = aiconfig.EnrollmentManager
	obj.SetLabels(labels)
	if len(annotations) > 0 {
		current := obj.GetAnnotations()
		if current == nil {
			current = map[string]string{}
		}
		for key, value := range annotations {
			current[key] = value
		}
		obj.SetAnnotations(current)
	}
	if reflect.DeepEqual(before.GetLabels(), obj.GetLabels()) && reflect.DeepEqual(before.GetAnnotations(), obj.GetAnnotations()) {
		return nil
	}
	return r.Patch(context.TODO(), obj, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}

func (r *ReconcileArgoCD) deleteAINotificationResource(cr *argoproj.ArgoCD, name string, obj client.Object) error {
	ctx := context.TODO()
	if err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: cr.Namespace}, obj); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !aiNotificationOwnedBy(cr, obj) {
		return nil
	}
	uid, version := obj.GetUID(), obj.GetResourceVersion()
	return client.IgnoreNotFound(r.Delete(ctx, obj, client.Preconditions{UID: &uid, ResourceVersion: &version}))
}

// reconcileAIEnrollment writes the authoritative enrollment only after the
// ArgoCD reconciler has passed its management selector and completed notification
// wiring. The shared AI controller never grants enrollment itself.
func (r *ReconcileArgoCD) reconcileAIEnrollment(cr *argoproj.ArgoCD, enroll bool) error {
	if cr.UID == "" {
		if !enroll {
			return nil // Without an identity, no existing enrollment belongs to us.
		}
		return errors.New("cannot enroll AI notifications without an ArgoCD UID")
	}
	ctx := context.TODO()
	operatorNamespace, err := argoutil.GetOperatorNamespace()
	if err != nil {
		return fmt.Errorf("determining AI enrollment namespace: %w", err)
	}
	var desired string
	if enroll {
		sa := &corev1.ServiceAccount{}
		if err := r.Get(ctx, types.NamespacedName{Name: aiNotificationsSAName, Namespace: cr.Namespace}, sa); err != nil {
			return err
		}
		if sa.UID == "" {
			return errors.New("cannot enroll AI notifications without a ServiceAccount UID")
		}
		value, err := json.Marshal(aiconfig.InstanceEnrollment{
			Name: cr.Name, UID: string(cr.UID), ServiceAccountUID: string(sa.UID),
		})
		if err != nil {
			return err
		}
		desired = string(value)
	}
	key := types.NamespacedName{Name: aiconfig.EnrollmentConfigMapName, Namespace: operatorNamespace}
	return retry.OnError(retry.DefaultRetry, func(err error) bool {
		return apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err)
	}, func() error {
		registry := &corev1.ConfigMap{}
		if err := r.Get(ctx, key, registry); apierrors.IsNotFound(err) {
			if !enroll {
				return nil
			}
			return r.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: key.Name, Namespace: key.Namespace,
					Labels: map[string]string{aiconfig.EnrollmentManagedByLabel: aiconfig.EnrollmentManager},
				},
				Data: map[string]string{cr.Namespace: desired},
			})
		} else if err != nil {
			return err
		}
		if registry.Labels[aiconfig.EnrollmentManagedByLabel] != aiconfig.EnrollmentManager {
			return fmt.Errorf("refusing to modify AI enrollment ConfigMap %s not managed by %s", key, aiconfig.EnrollmentManager)
		}
		before := registry.DeepCopy()
		if enroll {
			if registry.Data[cr.Namespace] == desired {
				return nil
			}
			if registry.Data == nil {
				registry.Data = map[string]string{}
			}
			registry.Data[cr.Namespace] = desired
		} else {
			value, exists := registry.Data[cr.Namespace]
			if !exists {
				return nil
			}
			var enrollment aiconfig.InstanceEnrollment
			if err := json.Unmarshal([]byte(value), &enrollment); err != nil {
				return fmt.Errorf("reading AI enrollment for %s: %w", cr.Namespace, err)
			}
			if enrollment.UID != string(cr.UID) {
				return nil
			}
			delete(registry.Data, cr.Namespace)
		}
		return r.Patch(ctx, registry, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	})
}
