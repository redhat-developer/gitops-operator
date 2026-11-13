package notificationsconfiguration

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"

	"github.com/redhat-developer/gitops-operator/argocd-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argoutil"
)

const (
	ArgoCDNotificationsConfigMap         = "argocd-notifications-cm"
	defaultNotificationsConfigurationName = "default-notifications-configuration"
	aiNotificationsConfigurationName     = "gitops-ai-controller-notifications"
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
	if defaultNC.Spec.Context != nil {
		expectedConfiguration["context"] = mapToString(defaultNC.Spec.Context)
	}

	// Merge the AI-managed NC CR when it exists.
	aiNC := &v1alpha1.NotificationsConfiguration{}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: aiNotificationsConfigurationName, Namespace: ns}, aiNC); err == nil {
		maps.Copy(expectedConfiguration, aiNC.Spec.Triggers)
		maps.Copy(expectedConfiguration, aiNC.Spec.Templates)
		maps.Copy(expectedConfiguration, aiNC.Spec.Services)
		// Subscriptions: concatenate both YAML arrays under the "subscriptions" key.
		aiSubs := aiNC.Spec.Subscriptions["subscriptions"]
		if aiSubs != "" {
			merged, err := concatSubscriptionYAML(expectedConfiguration["subscriptions"], aiSubs)
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
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get AI NotificationsConfiguration: %s", err)
	}

	// Context is checked separately because map serialization order is non-deterministic.
	changed := checkIfContextChanged(defaultNC, NotificationsConfigMap)
	for k := range expectedConfiguration {
		if !reflect.DeepEqual(expectedConfiguration[k], NotificationsConfigMap.Data[k]) && k != "context" {
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

// concatSubscriptionYAML appends the YAML array in b after the entries in a.
func concatSubscriptionYAML(a, b string) (string, error) {
	if a == "" {
		return b, nil
	}
	if b == "" {
		return a, nil
	}
	var aList, bList []any
	if err := yaml.Unmarshal([]byte(a), &aList); err != nil {
		return "", fmt.Errorf("parsing subscriptions %q: %w", a, err)
	}
	if err := yaml.Unmarshal([]byte(b), &bList); err != nil {
		return "", fmt.Errorf("parsing subscriptions %q: %w", b, err)
	}
	out, err := yaml.Marshal(append(aList, bList...))
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
