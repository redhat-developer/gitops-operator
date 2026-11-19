package controller

import (
	"context"
	"strings"

	"github.com/go-logr/logr"
	aiconfig "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/ai"
	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	triggerAutomatic = "automatic"

	// The expected caller SA name — a dedicated identity with no RBAC permissions,
	// used only by ArgoCD Notifications to call the webhook. The operator creates
	// one per AI-enabled ArgoCD namespace, so the name alone does not identify a
	// sender; the namespace it lives in does.
	notificationsSAName = aiconfig.NotificationsServiceAccountName

	serviceAccountUsernamePrefix = "system:serviceaccount:"
)

// CallerIdentity is the ServiceAccount the API server attributed the bearer
// token to. Every field comes from the TokenReview response, never from the
// request payload.
type CallerIdentity struct {
	// Username is the full attributed name, e.g.
	// "system:serviceaccount:team-a:gitops-ai-controller-notifications".
	Username string
	// Namespace is the verified ServiceAccount namespace. This identifies the
	// potential managing ArgoCD instance.
	Namespace string
	// Name is the verified ServiceAccount name.
	Name string
	// UID identifies the exact ServiceAccount verified by the API server.
	UID string
}

// AuthResult distinguishes authentication failure from authorization failure
// so the handler can return the correct HTTP status code.
type AuthResult struct {
	// Authenticated is true if the token was valid.
	Authenticated bool
	// Authorized is true if the caller is a notifications SA belonging to an
	// AI-enabled, unambiguous, operator-managed ArgoCD instance.
	Authorized bool
	// Reason is a short, stable explanation of an authorization denial. It is
	// empty when Authorized is true.
	Reason string
	// Identity is set whenever Authenticated is true.
	Identity CallerIdentity
	// Instance is the verified managing ArgoCD instance. Set only when
	// Authorized is true, and passed to downstream request processing so
	// Application-level authorization never has to infer it.
	Instance *argocdInstance
}

func denied(identity CallerIdentity, reason string) *AuthResult {
	return &AuthResult{Authenticated: true, Authorized: false, Reason: reason, Identity: identity}
}

// authorizeCaller verifies the bearer token via TokenReview (authentication),
// then resolves the verified ServiceAccount namespace to the operator-managed
// ArgoCD instance that is allowed to request diagnoses (authorization).
//
// A non-nil error means the decision could not be made — the handler must fail
// the request as a server error rather than treat it as a denial.
//
// TokenReview is performed without a custom audience: the operator injects a
// kubernetes.io/service-account-token Secret, which has no `aud` claim.
// Requiring audience "gitops-ai-controller" makes every webhook return 401.
func (s *TriggerService) authorizeCaller(ctx context.Context, token string, logger logr.Logger) (*AuthResult, error) {
	review := &authv1.TokenReview{
		Spec: authv1.TokenReviewSpec{
			Token: token,
		},
	}

	result, err := s.kubeClient.AuthenticationV1().TokenReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		logger.Error(err, "TokenReview API call failed")
		return nil, err
	}

	if !result.Status.Authenticated {
		logger.Info("token not authenticated", "error", result.Status.Error)
		return &AuthResult{Authenticated: false}, nil
	}

	username := result.Status.User.Username
	namespace, name, ok := parseServiceAccountUsername(username)
	identity := CallerIdentity{Username: username, Namespace: namespace, Name: name, UID: result.Status.User.UID}
	if !ok {
		logger.Info("caller authenticated but is not a service account", "username", username)
		return denied(identity, "caller is not a service account"), nil
	}

	// The SA name is the same in every ArgoCD namespace, so this narrows the
	// caller to "some instance's notifications controller" and nothing more.
	// The namespace lookup below is what identifies which instance.
	if name != notificationsSAName {
		logger.Info("caller authenticated but is not the notifications service account",
			"username", username, "expected", notificationsSAName)
		return denied(identity, "caller is not the notifications service account"), nil
	}

	instance, err := s.resolveManagingInstance(ctx, namespace)
	if err != nil {
		switch {
		case isAuthorizationDenied(err):
			logger.Info("caller authenticated but no eligible ArgoCD instance vouches for it",
				"username", username, "namespace", namespace, "reason", err.Error())
			return denied(identity, err.Error()), nil
		default:
			logger.Error(err, "could not resolve the managing ArgoCD instance", "namespace", namespace)
			return nil, err
		}
	}
	if identity.UID == "" || identity.UID != instance.ServiceAccountUID {
		return denied(identity, errInstanceNotEnrolled.Error()), nil
	}

	logger.V(1).Info("caller authenticated and authorized",
		"username", username, "argocd", instance.String())

	return &AuthResult{
		Authenticated: true,
		Authorized:    true,
		Identity:      identity,
		Instance:      instance,
	}, nil
}

// parseServiceAccountUsername splits "system:serviceaccount:<ns>:<name>".
// It reports false for any other principal, including real users and nodes.
func parseServiceAccountUsername(username string) (namespace, name string, ok bool) {
	rest, found := strings.CutPrefix(username, serviceAccountUsernamePrefix)
	if !found {
		return "", "", false
	}
	namespace, name, found = strings.Cut(rest, ":")
	if !found || namespace == "" || name == "" || strings.Contains(name, ":") {
		return "", "", false
	}
	return namespace, name, true
}

func extractBearerToken(authHeader string) string {
	if token, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
		return token
	}
	return ""
}
