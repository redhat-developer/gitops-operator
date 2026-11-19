package notificationsconfiguration

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"

	"github.com/redhat-developer/gitops-operator/argocd-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argoutil"
	aiconfig "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/ai"
)

const (
	ArgoCDNotificationsConfigMap          = "argocd-notifications-cm"
	defaultNotificationsConfigurationName = "default-notifications-configuration"
	aiNotificationsConfigurationName      = "gitops-ai-controller-notifications"
)

// reconcileNotificationsConfigmap ensures argocd-notifications-cm reflects the
// merged content of the default NotificationsConfiguration CR and the
// gitops-ai-controller-notifications CR (when present).
func (r *NotificationsConfigurationReconciler) reconcileNotificationsConfigmap(cr *v1alpha1.NotificationsConfiguration) error {
	ns := cr.Namespace

	// The default NC CR is the base and owns the ConfigMap.
	// When the reconcile is triggered by the AI NC CR, fetch the default explicitly.
	// When triggered by the default NC CR itself, cr is already the freshly-fetched default.
	defaultNC := cr
	if cr.Name != defaultNotificationsConfigurationName {
		defaultNC = &v1alpha1.NotificationsConfiguration{}
		if err := r.Get(context.TODO(), types.NamespacedName{Name: defaultNotificationsConfigurationName, Namespace: ns}, defaultNC); err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get default NotificationsConfiguration: %s", err)
			}
			// Default doesn't exist yet — nothing to merge into.
			return nil
		}
	}

	NotificationsConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ArgoCDNotificationsConfigMap,
			Namespace: ns,
		},
	}
	if err := argoutil.FetchObject(r.Client, ns, NotificationsConfigMap.Name, NotificationsConfigMap); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get the configmap %s : %s", NotificationsConfigMap.Name, err)
		}
		if err := controllerutil.SetControllerReference(defaultNC, NotificationsConfigMap, r.Scheme); err != nil {
			return err
		}
		argoutil.AddTrackedByOperatorLabel(&NotificationsConfigMap.ObjectMeta)
		argoutil.LogResourceCreation(log, NotificationsConfigMap)
		if err := r.Create(context.TODO(), NotificationsConfigMap); err != nil {
			return err
		}
	}

	// Build expected CM content from the default NC CR.
	expectedConfiguration := make(map[string]string)
	maps.Copy(expectedConfiguration, defaultNC.Spec.Triggers)
	maps.Copy(expectedConfiguration, defaultNC.Spec.Templates)
	maps.Copy(expectedConfiguration, defaultNC.Spec.Services)
	maps.Copy(expectedConfiguration, defaultNC.Spec.Subscriptions)
	mergedContext := maps.Clone(defaultNC.Spec.Context)
	if mergedContext != nil {
		expectedConfiguration["context"] = mapToString(mergedContext)
	}

	// Merge the AI-managed NC CR when it exists.
	aiNC := &v1alpha1.NotificationsConfiguration{}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: aiNotificationsConfigurationName, Namespace: ns}, aiNC); err == nil && isManagedAIConfiguration(aiNC) {
		maps.Copy(expectedConfiguration, aiNC.Spec.Triggers)
		maps.Copy(expectedConfiguration, aiNC.Spec.Templates)
		mergeManagedServices(expectedConfiguration, aiNC.Spec.Services)
		// Managed recipients belong exclusively to the operator. Preserve other
		// default subscriptions, including other recipients of the same trigger.
		aiSubs := aiNC.Spec.Subscriptions["subscriptions"]
		if aiSubs != "" {
			merged, err := mergeSubscriptionYAML(expectedConfiguration["subscriptions"], aiSubs, aiNC.Spec.Services)
			if err != nil {
				return fmt.Errorf("merging AI notification subscriptions: %w", err)
			}
			expectedConfiguration["subscriptions"] = merged
		}
		// Copy any other subscription keys the AI CR may carry.
		for k, v := range aiNC.Spec.Subscriptions {
			if k != "subscriptions" {
				expectedConfiguration[k] = v
			}
		}
		if aiNC.Spec.Context != nil {
			if mergedContext == nil {
				mergedContext = make(map[string]string)
			}
			maps.Copy(mergedContext, aiNC.Spec.Context)
			expectedConfiguration["context"] = mapToString(mergedContext)
		}
	} else if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get AI NotificationsConfiguration: %s", err)
	}

	// Compare both the keys and values: a removed AI entry must trigger an
	// update even if every remaining value is unchanged. Context comparison
	// ignores serialization order.
	changed := len(expectedConfiguration) != len(NotificationsConfigMap.Data)
	for k, expected := range expectedConfiguration {
		actual, exists := NotificationsConfigMap.Data[k]
		if !exists {
			changed = true
		} else if k == "context" {
			contextNC := &v1alpha1.NotificationsConfiguration{Spec: v1alpha1.NotificationsConfigurationSpec{Context: mergedContext}}
			changed = changed || checkIfContextChanged(contextNC, NotificationsConfigMap)
		} else if actual != expected {
			changed = true
		}
	}

	if changed {
		NotificationsConfigMap.Data = expectedConfiguration
		argoutil.LogResourceUpdate(log, NotificationsConfigMap, "updating config map data")
		if err := r.Update(context.TODO(), NotificationsConfigMap); err != nil {
			return err
		}
	}

	return nil
}

// Notifications resolves services by their destination name, ignoring the type.
// Remove aliases before overlaying operator definitions so map iteration order
// cannot let a user-defined service replace an operator-managed webhook.
func mergeManagedServices(configuration, managedServices map[string]string) {
	managedNames := make(map[string]bool)
	for key := range managedServices {
		if name := notificationServiceName(key); name != "" {
			managedNames[name] = true
		}
	}
	for key := range configuration {
		if managedNames[notificationServiceName(key)] {
			delete(configuration, key)
		}
	}
	maps.Copy(configuration, managedServices)
}

func notificationServiceName(key string) string {
	parts := strings.Split(key, ".")
	if parts[0] != "service" {
		return ""
	}
	switch len(parts) {
	case 2:
		return parts[1]
	case 3:
		return parts[2]
	default:
		return ""
	}
}

// A matching name alone does not make a user-created CR operator-managed.
// The ArgoCD reconciler verifies its exact instance UID before modifying it;
// this merger only accepts CRs carrying both management and ownership metadata.
func isManagedAIConfiguration(cr *v1alpha1.NotificationsConfiguration) bool {
	if cr.Labels[aiconfig.EnrollmentManagedByLabel] != aiconfig.EnrollmentManager {
		return false
	}
	owner := metav1.GetControllerOf(cr)
	if owner == nil || owner.Kind != "ArgoCD" || owner.Name == "" || owner.UID == "" {
		return false
	}
	group, version, ok := strings.Cut(owner.APIVersion, "/")
	return ok && group == "argoproj.io" && version != ""
}

type notificationSubscription struct {
	Recipients []string `json:"recipients"`
	Triggers   []string `json:"triggers,omitempty"`
	Selector   string   `json:"selector,omitempty"`
}

// mergeSubscriptionYAML reserves managed recipients for the operator, regardless
// of the user's trigger or selector. Webhooks ignore the recipient suffix after
// ':', so all aliases of a managed webhook destination are reserved. Other
// services retain recipient-specific routing, such as distinct Slack channels.
// A default subscription can contain several recipients; only the managed ones
// are removed. Neither source CR is modified.
func mergeSubscriptionYAML(defaultYAML, managedYAML string, managedServices map[string]string) (string, error) {
	var defaults, managed []notificationSubscription
	if err := yaml.Unmarshal([]byte(defaultYAML), &defaults); err != nil {
		return "", fmt.Errorf("parsing default subscriptions: %w", err)
	}
	if err := yaml.Unmarshal([]byte(managedYAML), &managed); err != nil {
		return "", fmt.Errorf("parsing managed subscriptions: %w", err)
	}
	managedRecipients := make(map[string]bool)
	managedWebhooks := make(map[string]bool)
	for _, subscription := range managed {
		for _, recipient := range subscription.Recipients {
			managedRecipients[recipient] = true
			service, _, _ := strings.Cut(recipient, ":")
			if _, isWebhook := managedServices["service.webhook."+service]; isWebhook {
				managedWebhooks[service] = true
			}
		}
	}
	merged := make([]notificationSubscription, 0, len(defaults)+len(managed))
	for _, subscription := range defaults {
		subscription.Recipients = slices.DeleteFunc(subscription.Recipients, func(recipient string) bool {
			service, _, _ := strings.Cut(recipient, ":")
			return managedRecipients[recipient] || managedWebhooks[service]
		})
		if len(subscription.Recipients) > 0 {
			merged = append(merged, subscription)
		}
	}
	out, err := yaml.Marshal(append(merged, managed...))
	return string(out), err
}

func mapToString(m map[string]string) string {
	var result strings.Builder
	for key, value := range m {
		fmt.Fprintf(&result, "%s: %s\n", key, value)
	}
	return result.String()
}

// checkIfContextChanged checks if context value in NotificationConfiguration and notificationConfigMap context have same value
// return true if there is difference, and false if no changes observed
func checkIfContextChanged(cr *v1alpha1.NotificationsConfiguration, notificationConfigMap *corev1.ConfigMap) bool {
	if len(cr.Spec.Context) == 0 {
		return notificationConfigMap.Data["context"] != ""
	}
	cmContext := strings.Split(strings.TrimSuffix(notificationConfigMap.Data["context"], "\n"), "\n")
	if len(cmContext) == len(cr.Spec.Context) {
		// Create a map for quick lookups
		stringMap := make(map[string]bool)
		for _, item := range cmContext {
			stringMap[item] = true
		}

		// Check for each item in array1
		for key, value := range cr.Spec.Context {
			if !stringMap[fmt.Sprintf("%s: %s", key, value)] {
				return true
			}
		}
	} else {
		return true
	}
	return false
}
