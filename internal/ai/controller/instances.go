package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	aiconfig "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/ai"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
)

// Authorization failures that are a property of the cluster state rather than
// of the API call. Each one denies the request; none of them is retryable.
var (
	// errNoArgoCDInstance means the verified caller's namespace holds no
	// operator-managed ArgoCD instance, so no instance can vouch for it.
	errNoArgoCDInstance = errors.New("no ArgoCD instance in the caller namespace")
	// errAmbiguousArgoCDInstance means the namespace holds more than one
	// instance. The notifications ServiceAccount and its token are per
	// namespace, not per instance, so the sender cannot be told apart. Reject
	// rather than pick one.
	errAmbiguousArgoCDInstance = errors.New("more than one ArgoCD instance in the caller namespace")
	// errAITroubleshootingDisabled means the resolved instance did not opt in
	// via .spec.ai.troubleshooting.
	errAITroubleshootingDisabled = errors.New("AI troubleshooting is not enabled for the ArgoCD instance")
	// errApplicationNotOwned means ownership of the requested Application by
	// the verified instance could not be established.
	errApplicationNotOwned = errors.New("application ownership could not be established for the ArgoCD instance")
	errInstanceNotEnrolled = errors.New("ArgoCD instance or notification identity is not enrolled by this operator")
)

func isAuthorizationDenied(err error) bool {
	return errors.Is(err, errNoArgoCDInstance) || errors.Is(err, errAmbiguousArgoCDInstance) ||
		errors.Is(err, errAITroubleshootingDisabled) || errors.Is(err, errApplicationNotOwned) ||
		errors.Is(err, errInstanceNotEnrolled)
}

// argocdInstance is the operator-managed ArgoCD instance that a webhook caller
// was verified to belong to. It is derived from the TokenReview result only,
// never from the request payload.
type argocdInstance struct {
	Name              string
	Namespace         string
	UID               string
	ServiceAccountUID string
	// SourceNamespaces mirrors .spec.sourceNamespaces so Application-level
	// authorization can enforce Applications in Any Namespace. Phase 2 records
	// it; it is not yet used to admit requests.
	SourceNamespaces []string
}

func (i *argocdInstance) String() string {
	if i == nil {
		return ""
	}
	return i.Namespace + "/" + i.Name
}

// resolveManagingInstance returns the single operator-managed ArgoCD instance
// in namespace that has AI troubleshooting enabled.
//
// namespace must come from a verified TokenReview identity. A returned error
// that is one of the sentinels above is a denial; any other error is an API
// failure the caller should surface as a server error rather than a denial.
func (s *TriggerService) resolveManagingInstance(ctx context.Context, namespace string) (*argocdInstance, error) {
	selector, err := labels.Parse(s.config.ArgoCDLabelSelector)
	if err != nil {
		return nil, fmt.Errorf("invalid ArgoCD label selector: %w", err)
	}
	list, err := s.dynClient.Resource(argocdGVR).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, fmt.Errorf("listing ArgoCD instances in %s: %w", namespace, err)
	}

	// A terminating instance is on its way out and must not vouch for a caller.
	live := make([]*unstructured.Unstructured, 0, len(list.Items))
	for i := range list.Items {
		if list.Items[i].GetDeletionTimestamp() == nil && selector.Matches(labels.Set(list.Items[i].GetLabels())) {
			live = append(live, &list.Items[i])
		}
	}

	switch len(live) {
	case 0:
		return nil, errNoArgoCDInstance
	case 1:
	default:
		return nil, errAmbiguousArgoCDInstance
	}

	cr := live[0]
	if enabled, _, _ := unstructured.NestedBool(cr.Object, "spec", "ai", "troubleshooting"); !enabled {
		return nil, errAITroubleshootingDisabled
	}
	if s.config.OperatorNamespace == "" {
		return nil, fmt.Errorf("operator namespace is required to verify enrollment")
	}
	registry, err := s.kubeClient.CoreV1().ConfigMaps(s.config.OperatorNamespace).Get(ctx, aiconfig.EnrollmentConfigMapName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, errInstanceNotEnrolled
	}
	if err != nil {
		return nil, fmt.Errorf("reading AI instance enrollments: %w", err)
	}
	if registry.Labels[aiconfig.EnrollmentManagedByLabel] != aiconfig.EnrollmentManager {
		return nil, errInstanceNotEnrolled
	}
	var enrollment aiconfig.InstanceEnrollment
	if err := json.Unmarshal([]byte(registry.Data[namespace]), &enrollment); err != nil {
		return nil, errInstanceNotEnrolled
	}
	if enrollment.Name != cr.GetName() || enrollment.UID == "" || enrollment.UID != string(cr.GetUID()) || enrollment.ServiceAccountUID == "" {
		return nil, errInstanceNotEnrolled
	}

	sourceNamespaces, _, _ := unstructured.NestedStringSlice(cr.Object, "spec", "sourceNamespaces")
	return &argocdInstance{
		Name:              cr.GetName(),
		Namespace:         cr.GetNamespace(),
		UID:               enrollment.UID,
		ServiceAccountUID: enrollment.ServiceAccountUID,
		SourceNamespaces:  sourceNamespaces,
	}, nil
}

// authorizeApplication decides whether the verified ArgoCD instance may request
// a diagnosis of the named Application.
//
// This is the seam that full ownership and AppProject authorization replaces.
// Until that lands, the only ownership this can establish without guessing is
// an Application in the instance's own namespace: Applications in Any Namespace
// entries may be glob patterns, and matching them loosely here would admit
// Applications the instance does not actually manage. Everything else is
// refused, so the gap fails closed rather than open.
func authorizeApplication(instance *argocdInstance, appNamespace string) error {
	if instance == nil {
		return errApplicationNotOwned
	}
	if appNamespace == instance.Namespace {
		return nil
	}
	return errApplicationNotOwned
}
