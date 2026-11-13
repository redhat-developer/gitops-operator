package argocd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/redhat-developer/gitops-operator/argocd-operator/api/v1alpha1"
	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	aiconfig "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/ai"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argoutil"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	aiNotifTriggerKey            = "trigger.on-gitops-ai-diagnose"
	aiNotifTemplateKey           = "template.gitops-ai-diagnose"
	aiNotifServiceKey            = "service.webhook.gitops-ai-controller"
	aiTriggerTokenSecretName     = "gitops-ai-controller-notifications-token" // #nosec G101 -- resource name, not a credential
	aiNotificationsSecretName    = "argocd-notifications-secret"              // #nosec G101 -- resource name, not a credential
	aiTriggerTokenKey            = "gitops-ai-controller-token" // #nosec G101 -- Secret data key, not a credential
	aiTLSCertsConfigMapName      = "argocd-tls-certs-cm"
	aiCABundleKey                = "service-ca.crt"
	aiCAConfigMapName            = "gitops-ai-controller-serving-ca"
	aiNotificationsSAName        = "gitops-ai-controller-notifications"
	aiTriggerServiceName         = aiconfig.ServiceName
	aiWebhookName                = "gitops-ai-controller"
	aiTriggerTLSEnv              = "GITOPS_AI_CONTROLLER_TLS_ENABLED"
	enableAIFeaturesEnv          = "GITOPS_AI_ENABLED"
	aiNotificationsConfigurationName = "gitops-ai-controller-notifications"
	aiCAInjectAnnotation         = "service.beta.openshift.io/inject-cabundle"
)

// Missing dependencies are expected while the service-account and service-CA
// controllers populate their resources. The reconciler schedules a short retry.
var errAINotificationsPending = errors.New("waiting for AI notification configuration, token or serving CA")

// aiNotificationDependencyRequests routes token and CA changes to ArgoCD so it
// can propagate updated credentials into the notifications secret/configmap.
func (r *ReconcileArgoCD) aiNotificationDependencyRequests(ctx context.Context, obj client.Object) []reconcile.Request {
	switch obj.(type) {
	case *corev1.Secret:
		if obj.GetName() != aiTriggerTokenSecretName && obj.GetName() != aiNotificationsSecretName {
			return nil
		}
	case *corev1.ConfigMap:
		if obj.GetName() != aiCAConfigMapName && obj.GetName() != aiTLSCertsConfigMapName {
			return nil
		}
	default:
		return nil
	}
	instances := &argoproj.ArgoCDList{}
	if err := r.List(ctx, instances, client.InNamespace(obj.GetNamespace())); err != nil {
		log.Error(err, "unable to find ArgoCD instances for AI dependency update")
		return nil
	}
	requests := make([]reconcile.Request, 0, len(instances.Items))
	for _, instance := range instances.Items {
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
		Triggers:  map[string]string{aiNotifTriggerKey: aiNotificationTrigger()},
		Templates: map[string]string{aiNotifTemplateKey: "webhook:\n  gitops-ai-controller:\n    method: POST\n    path: /api/v1/diagnose\n    body: |\n      {\"application\":\"{{.app.metadata.name}}\",\"applicationNamespace\":\"{{.app.metadata.namespace}}\"}\n"},
		Services:  map[string]string{aiNotifServiceKey: fmt.Sprintf("url: %s://%s.%s.svc:%d\nheaders:\n- name: Authorization\n  value: Bearer $gitops-ai-controller-token\n", scheme, aiTriggerServiceName, triggerNS, port)},
		Subscriptions: map[string]string{"subscriptions": "- recipients:\n  - gitops-ai-controller\n  triggers:\n  - on-gitops-ai-diagnose\n"},
	}

	aiNC := &v1alpha1.NotificationsConfiguration{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: aiNotificationsConfigurationName, Namespace: ns}, aiNC)
	if apierrors.IsNotFound(err) {
		if err := r.Client.Create(context.TODO(), &v1alpha1.NotificationsConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: aiNotificationsConfigurationName, Namespace: ns},
			Spec:       aiSpec,
		}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !reflect.DeepEqual(aiNC.Spec, aiSpec) {
		before := aiNC.DeepCopy()
		aiNC.Spec = aiSpec
		if err := r.Client.Patch(context.TODO(), aiNC, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}

	// Ensure the notifications SA exists in the ArgoCD namespace.
	sa := &corev1.ServiceAccount{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aiNotificationsSAName, Namespace: ns}, sa); apierrors.IsNotFound(err) {
		if err := r.Client.Create(context.TODO(), &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{Name: aiNotificationsSAName, Namespace: ns},
		}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	// Ensure the SA token Secret exists so Kubernetes can populate it.
	tokenSec := &corev1.Secret{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aiTriggerTokenSecretName, Namespace: ns}, tokenSec); apierrors.IsNotFound(err) {
		if err := r.Client.Create(context.TODO(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:        aiTriggerTokenSecretName,
				Namespace:   ns,
				Annotations: map[string]string{"kubernetes.io/service-account.name": aiNotificationsSAName},
			},
			Type: corev1.SecretTypeServiceAccountToken,
		}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	// Ensure the serving CA ConfigMap exists (TLS only); OpenShift injects the CA bundle.
	if tlsEnabled {
		caMap := &corev1.ConfigMap{}
		if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aiCAConfigMapName, Namespace: ns}, caMap); apierrors.IsNotFound(err) {
			if err := r.Client.Create(context.TODO(), &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:        aiCAConfigMapName,
					Namespace:   ns,
					Annotations: map[string]string{aiCAInjectAnnotation: "true"},
				},
			}); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}

	// Propagate the SA token into argocd-notifications-secret.
	token := &corev1.Secret{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aiTriggerTokenSecretName, Namespace: ns}, token); err != nil {
		if apierrors.IsNotFound(err) {
			return errAINotificationsPending
		}
		return err
	}
	if len(token.Data["token"]) == 0 {
		return errAINotificationsPending
	}
	notifSecret := &corev1.Secret{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aiNotificationsSecretName, Namespace: ns}, notifSecret); err != nil {
		if apierrors.IsNotFound(err) {
			return errAINotificationsPending
		}
		return err
	}
	if string(notifSecret.Data[aiTriggerTokenKey]) != string(token.Data["token"]) {
		before := notifSecret.DeepCopy()
		if notifSecret.Data == nil {
			notifSecret.Data = map[string][]byte{}
		}
		notifSecret.Data[aiTriggerTokenKey] = token.Data["token"]
		if err := r.Client.Patch(context.TODO(), notifSecret, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}

	if !tlsEnabled {
		return nil
	}

	// Propagate the serving CA into argocd-tls-certs-cm.
	ca := &corev1.ConfigMap{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aiCAConfigMapName, Namespace: ns}, ca); err != nil {
		if apierrors.IsNotFound(err) {
			return errAINotificationsPending
		}
		return err
	}
	if ca.Data[aiCABundleKey] == "" {
		return errAINotificationsPending
	}
	certs := &corev1.ConfigMap{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aiTLSCertsConfigMapName, Namespace: ns}, certs); err != nil {
		if apierrors.IsNotFound(err) {
			return errAINotificationsPending
		}
		return err
	}
	host := fmt.Sprintf("%s.%s.svc", aiTriggerServiceName, triggerNS)
	if certs.Data[host] != ca.Data[aiCABundleKey] {
		before := certs.DeepCopy()
		if certs.Data == nil {
			certs.Data = map[string]string{}
		}
		certs.Data[host] = ca.Data[aiCABundleKey]
		if err := r.Client.Patch(context.TODO(), certs, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}
	return nil
}

func (r *ReconcileArgoCD) cleanupAINotifications(cr *argoproj.ArgoCD) error {
	ns := cr.Namespace

	// Delete the AI NC CR; the NC controller will rebuild argocd-notifications-cm
	// without AI entries on its next reconcile.
	aiNC := &v1alpha1.NotificationsConfiguration{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aiNotificationsConfigurationName, Namespace: ns}, aiNC); err == nil {
		if err := r.Client.Delete(context.TODO(), aiNC); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Remove the SA token from argocd-notifications-secret.
	secret := &corev1.Secret{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aiNotificationsSecretName, Namespace: ns}, secret); err == nil {
		if _, exists := secret.Data[aiTriggerTokenKey]; exists {
			before := secret.DeepCopy()
			delete(secret.Data, aiTriggerTokenKey)
			if err := r.Client.Patch(context.TODO(), secret, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
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
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aiTLSCertsConfigMapName, Namespace: ns}, certs); err == nil {
		host := fmt.Sprintf("%s.%s.svc", aiTriggerServiceName, triggerNS)
		if _, exists := certs.Data[host]; exists {
			before := certs.DeepCopy()
			delete(certs.Data, host)
			if err := r.Client.Patch(context.TODO(), certs, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
				return err
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Delete the token Secret, notifications SA, and CA ConfigMap.
	tokenSec := &corev1.Secret{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aiTriggerTokenSecretName, Namespace: ns}, tokenSec); err == nil {
		if err := r.Client.Delete(context.TODO(), tokenSec); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	sa := &corev1.ServiceAccount{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aiNotificationsSAName, Namespace: ns}, sa); err == nil {
		if err := r.Client.Delete(context.TODO(), sa); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	caMap := &corev1.ConfigMap{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aiCAConfigMapName, Namespace: ns}, caMap); err == nil {
		if err := r.Client.Delete(context.TODO(), caMap); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

// aiNotificationTrigger returns the YAML for the on-gitops-ai-diagnose trigger.
// The epoch-based oncePer key allows redelivery when Notifications re-evaluates
// an Application after a lost webhook without scheduling that re-evaluation itself.
func aiNotificationTrigger() string {
	once := "  oncePer: app.metadata.uid + ':' + toJSON(app.status.sync.revisions ?? [app.status.sync.revision ?? '']) + ':' + (app.status.health.lastTransitionTime ?? '') + ':' + string(int(time.Now().Unix() / 60))\n"
	var b strings.Builder
	for _, h := range []string{"Degraded", "Unknown", "Missing", "Progressing", "Healthy", "Suspended"} {
		fmt.Fprintf(&b, "- when: app.status.health.status == '%s'\n", h)
		b.WriteString(once + "  send: [gitops-ai-diagnose]\n")
	}
	return b.String()
}

