package controller

import (
	"context"
	"fmt"
	"strings"

	aiconfig "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/ai"
	"github.com/go-logr/logr"
	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	triggerAutomatic = "automatic"

	// The expected caller SA name — a dedicated identity with no RBAC permissions,
	// used only by ArgoCD Notifications to call the webhook.
	notificationsSAName = aiconfig.NotificationsServiceAccountName
)

// AuthResult distinguishes authentication failure from authorization failure
// so the handler can return the correct HTTP status code.
type AuthResult struct {
	// Authenticated is true if the token was valid.
	Authenticated bool
	// Authorized is true if the caller is the expected SA.
	Authorized bool
}

// authenticateCaller verifies the bearer token via TokenReview (authentication)
// and checks that the caller is the dedicated notifications SA (authorization).
//
// TokenReview is performed without a custom audience: the operator injects a
// kubernetes.io/service-account-token Secret, which has no `aud` claim.
// Requiring audience "gitops-ai-controller" makes every webhook return 401.
func authenticateCaller(ctx context.Context, client kubernetes.Interface, token string, allowedNS string, logger logr.Logger) *AuthResult {
	review := &authv1.TokenReview{
		Spec: authv1.TokenReviewSpec{
			Token: token,
		},
	}

	result, err := client.AuthenticationV1().TokenReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		logger.Error(err, "TokenReview API call failed")
		return nil // signals server error to caller
	}

	if !result.Status.Authenticated {
		logger.Info("token not authenticated", "error", result.Status.Error)
		return &AuthResult{Authenticated: false}
	}

	username := result.Status.User.Username

	// Authorization: only the dedicated notifications SA is allowed
	expectedSA := fmt.Sprintf("system:serviceaccount:%s:%s", allowedNS, notificationsSAName)
	if username != expectedSA {
		logger.Info("caller authenticated but not authorized", "username", username, "expected", expectedSA)
		return &AuthResult{Authenticated: true, Authorized: false}
	}

	logger.V(1).Info("caller authenticated and authorized", "username", username)

	return &AuthResult{
		Authenticated: true,
		Authorized:    true,
	}
}

func extractBearerToken(authHeader string) string {
	if token, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
		return token
	}
	return ""
}
